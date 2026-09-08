package chaintrail

import (
	"fmt"
	"os"
)

// JournalStats describes a journal only after a full integrity scan succeeds.
type JournalStats struct {
	JournalID       string            `json:"journal_id"`
	HeadSeq         uint64            `json:"head_seq"`
	HeadHash        string            `json:"head_hash"`
	JournalBytes    int64             `json:"journal_bytes"`
	PayloadBytes    uint64            `json:"payload_bytes"`
	DistinctKinds   int               `json:"distinct_kinds"`
	RecordsByKind   map[string]uint64 `json:"records_by_kind"`
	FirstRecordTime string            `json:"first_record_time,omitempty"`
	LastRecordTime  string            `json:"last_record_time,omitempty"`
}

// Stats performs a complete verification pass and derives operational metrics
// from the validated records. The head cache is never trusted for these values.
func (j *Journal) Stats() (JournalStats, error) {
	meta, err := j.loadMeta()
	if err != nil {
		return JournalStats{}, err
	}
	lock, err := acquireFileLock(j.lockPath(), false)
	if err != nil {
		return JournalStats{}, err
	}
	defer lock.Close()

	file, err := os.Open(j.journalPath())
	if err != nil {
		return JournalStats{}, fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return JournalStats{}, fmt.Errorf("stat journal: %w", err)
	}

	stats := JournalStats{
		JournalID:     meta.JournalID,
		JournalBytes:  info.Size(),
		RecordsByKind: make(map[string]uint64),
	}
	result, err := scanRecords(file, meta, func(record Record) error {
		stats.RecordsByKind[record.Kind]++
		stats.PayloadBytes += uint64(len(record.Payload))
		if stats.FirstRecordTime == "" {
			stats.FirstRecordTime = record.Time
		}
		stats.LastRecordTime = record.Time
		return nil
	})
	if err != nil {
		return JournalStats{}, err
	}
	stats.HeadSeq = result.Seq
	stats.HeadHash = result.Hash
	stats.DistinctKinds = len(stats.RecordsByKind)
	return stats, nil
}
