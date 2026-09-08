package chaintrail

import (
	"errors"
	"fmt"
	"io"
	"os"
)

type VerifyOptions struct {
	ExpectHash string
	Checkpoint *Checkpoint
}

type VerifyResult struct {
	JournalID string `json:"journal_id"`
	Seq       uint64 `json:"seq"`
	Hash      string `json:"hash"`
}

func (j *Journal) Verify(options VerifyOptions) (VerifyResult, error) {
	meta, err := j.loadMeta()
	if err != nil {
		return VerifyResult{}, err
	}
	lock, err := acquireFileLock(j.lockPath(), false)
	if err != nil {
		return VerifyResult{}, err
	}
	defer lock.Close()
	return j.verifyUnlocked(meta, options, nil)
}

func (j *Journal) verifyUnlocked(meta Meta, options VerifyOptions, visit func(Record) error) (VerifyResult, error) {
	if options.Checkpoint != nil && options.Checkpoint.JournalID != meta.JournalID {
		return VerifyResult{}, integrityf("checkpoint belongs to journal %s, not %s", options.Checkpoint.JournalID, meta.JournalID)
	}
	file, err := os.Open(j.journalPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return VerifyResult{}, ErrNotInitialized
		}
		return VerifyResult{}, fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()

	checkpointMatched := options.Checkpoint == nil
	if options.Checkpoint != nil && options.Checkpoint.Seq == 0 {
		anchor, anchorErr := anchorHash(meta)
		if anchorErr != nil {
			return VerifyResult{}, anchorErr
		}
		checkpointMatched = options.Checkpoint.Hash == anchor
		if !checkpointMatched {
			return VerifyResult{}, integrityf("checkpoint hash mismatch at sequence 0")
		}
	}
	combinedVisit := func(record Record) error {
		if options.Checkpoint != nil && record.Seq == options.Checkpoint.Seq {
			if record.Hash != options.Checkpoint.Hash {
				return integrityf("checkpoint hash mismatch at sequence %d", record.Seq)
			}
			checkpointMatched = true
		}
		if visit != nil {
			return visit(record)
		}
		return nil
	}
	result, err := scanRecords(file, meta, combinedVisit)
	if err != nil {
		return VerifyResult{}, err
	}
	if options.Checkpoint != nil && !checkpointMatched {
		return VerifyResult{}, integrityf("checkpoint sequence %d is beyond journal head %d", options.Checkpoint.Seq, result.Seq)
	}
	if options.ExpectHash != "" && result.Hash != options.ExpectHash {
		return VerifyResult{}, integrityf("head hash mismatch: expected %s, got %s", options.ExpectHash, result.Hash)
	}
	return result, nil
}

type RepairResult struct {
	Repaired     bool   `json:"repaired"`
	RemovedBytes int64  `json:"removed_bytes"`
	Seq          uint64 `json:"seq"`
	Hash         string `json:"hash"`
}

func (j *Journal) Repair() (RepairResult, error) {
	meta, err := j.loadMeta()
	if err != nil {
		return RepairResult{}, err
	}
	lock, err := acquireFileLock(j.lockPath(), true)
	if err != nil {
		return RepairResult{}, err
	}
	defer lock.Close()

	file, err := os.OpenFile(j.journalPath(), os.O_RDWR, 0)
	if err != nil {
		return RepairResult{}, fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return RepairResult{}, fmt.Errorf("stat journal: %w", err)
	}
	if info.Size() == 0 {
		result, err := scanRecords(io.NewSectionReader(file, 0, 0), meta, nil)
		if err != nil {
			return RepairResult{}, err
		}
		if err := atomicWriteJSON(j.headPath(), headCache{Seq: result.Seq, Hash: result.Hash, Size: 0}, 0o640); err != nil {
			return RepairResult{}, err
		}
		return RepairResult{Seq: result.Seq, Hash: result.Hash}, nil
	}

	last := make([]byte, 1)
	if _, err := file.ReadAt(last, info.Size()-1); err != nil {
		return RepairResult{}, fmt.Errorf("read journal tail: %w", err)
	}
	if last[0] == '\n' {
		result, err := scanRecords(io.NewSectionReader(file, 0, info.Size()), meta, nil)
		if err != nil {
			return RepairResult{}, err
		}
		if err := atomicWriteJSON(j.headPath(), headCache{Seq: result.Seq, Hash: result.Hash, Size: info.Size()}, 0o640); err != nil {
			return RepairResult{}, err
		}
		return RepairResult{Seq: result.Seq, Hash: result.Hash}, nil
	}

	prefixSize, err := findLastNewline(file, info.Size())
	if err != nil {
		return RepairResult{}, err
	}
	result, err := scanRecords(io.NewSectionReader(file, 0, prefixSize), meta, nil)
	if err != nil {
		return RepairResult{}, fmt.Errorf("refusing repair because intact prefix is invalid: %w", err)
	}
	removed := info.Size() - prefixSize
	if err := file.Truncate(prefixSize); err != nil {
		return RepairResult{}, fmt.Errorf("truncate incomplete tail: %w", err)
	}
	if err := file.Sync(); err != nil {
		return RepairResult{}, fmt.Errorf("sync repaired journal: %w", err)
	}
	if err := atomicWriteJSON(j.headPath(), headCache{Seq: result.Seq, Hash: result.Hash, Size: prefixSize}, 0o640); err != nil {
		return RepairResult{}, err
	}
	return RepairResult{Repaired: true, RemovedBytes: removed, Seq: result.Seq, Hash: result.Hash}, nil
}

func findLastNewline(file *os.File, size int64) (int64, error) {
	const block = int64(64 * 1024)
	end := size
	for end > 0 {
		start := end - block
		if start < 0 {
			start = 0
		}
		buf := make([]byte, end-start)
		if _, err := file.ReadAt(buf, start); err != nil {
			return 0, fmt.Errorf("scan journal tail: %w", err)
		}
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return start + int64(i) + 1, nil
			}
		}
		end = start
	}
	return 0, nil
}

func (j *Journal) Tail(n int) ([]Record, error) {
	if n < 0 {
		return nil, fmt.Errorf("tail count must be non-negative")
	}
	meta, err := j.loadMeta()
	if err != nil {
		return nil, err
	}
	lock, err := acquireFileLock(j.lockPath(), false)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if n == 0 {
		_, err := j.verifyUnlocked(meta, VerifyOptions{}, nil)
		return []Record{}, err
	}
	ring := make([]Record, 0, n)
	_, err = j.verifyUnlocked(meta, VerifyOptions{}, func(record Record) error {
		if len(ring) < n {
			ring = append(ring, record)
			return nil
		}
		copy(ring, ring[1:])
		ring[len(ring)-1] = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ring, nil
}
