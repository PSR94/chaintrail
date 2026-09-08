package chaintrail

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	FormatVersion   = 1
	MaxPayloadBytes = 1 << 20
	maxRecordBytes  = MaxPayloadBytes + 64*1024
)

var kindPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

type Meta struct {
	Version   int    `json:"version"`
	JournalID string `json:"journal_id"`
	CreatedAt string `json:"created_at"`
}

type Record struct {
	Seq     uint64          `json:"seq"`
	Time    string          `json:"time"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
	Prev    string          `json:"prev"`
	Hash    string          `json:"hash"`
}

type recordWithoutHash struct {
	Seq     uint64          `json:"seq"`
	Time    string          `json:"time"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
	Prev    string          `json:"prev"`
}

type headCache struct {
	Seq  uint64 `json:"seq"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type Journal struct {
	Dir string
	now func() time.Time
}

func Open(dir string) *Journal {
	if dir == "" {
		dir = ".chaintrail"
	}
	return &Journal{Dir: dir, now: time.Now}
}

func (j *Journal) metaPath() string    { return filepath.Join(j.Dir, "meta.json") }
func (j *Journal) journalPath() string { return filepath.Join(j.Dir, "journal.ndjson") }
func (j *Journal) headPath() string    { return filepath.Join(j.Dir, "head.json") }
func (j *Journal) lockPath() string    { return filepath.Join(j.Dir, "journal.lock") }

func (j *Journal) Init() (Meta, error) {
	if _, err := os.Stat(j.metaPath()); err == nil {
		return Meta{}, ErrAlreadyInitialized
	} else if !errors.Is(err, os.ErrNotExist) {
		return Meta{}, fmt.Errorf("inspect journal metadata: %w", err)
	}
	if err := os.MkdirAll(j.Dir, 0o750); err != nil {
		return Meta{}, fmt.Errorf("create journal directory: %w", err)
	}

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Meta{}, fmt.Errorf("generate journal id: %w", err)
	}
	meta := Meta{
		Version:   FormatVersion,
		JournalID: hex.EncodeToString(idBytes),
		CreatedAt: j.now().UTC().Format(time.RFC3339Nano),
	}
	if err := atomicWriteJSON(j.metaPath(), meta, 0o640); err != nil {
		return Meta{}, err
	}
	file, err := os.OpenFile(j.journalPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return Meta{}, fmt.Errorf("create journal file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return Meta{}, fmt.Errorf("sync journal file: %w", err)
	}
	if err := file.Close(); err != nil {
		return Meta{}, fmt.Errorf("close journal file: %w", err)
	}
	anchor, err := anchorHash(meta)
	if err != nil {
		return Meta{}, err
	}
	if err := atomicWriteJSON(j.headPath(), headCache{Seq: 0, Hash: anchor, Size: 0}, 0o640); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

func (j *Journal) Append(kind string, payload []byte) (Record, error) {
	if !kindPattern.MatchString(kind) {
		return Record{}, fmt.Errorf("invalid kind %q: use 1-64 letters, digits, '.', '_', ':', or '-'", kind)
	}
	if len(payload) == 0 {
		return Record{}, fmt.Errorf("payload must not be empty")
	}
	if len(payload) > MaxPayloadBytes {
		return Record{}, fmt.Errorf("payload exceeds %d bytes", MaxPayloadBytes)
	}
	canonicalPayload, err := canonicalJSON(payload)
	if err != nil {
		return Record{}, err
	}

	meta, err := j.loadMeta()
	if err != nil {
		return Record{}, err
	}
	lock, err := acquireFileLock(j.lockPath(), true)
	if err != nil {
		return Record{}, err
	}
	defer lock.Close()

	info, err := os.Stat(j.journalPath())
	if err != nil {
		return Record{}, fmt.Errorf("stat journal: %w", err)
	}
	head, err := j.loadTrustedHead(meta, info.Size())
	if err != nil {
		return Record{}, err
	}

	record := Record{
		Seq:     head.Seq + 1,
		Time:    j.now().UTC().Format(time.RFC3339Nano),
		Kind:    kind,
		Payload: canonicalPayload,
		Prev:    head.Hash,
	}
	record.Hash, err = hashRecord(record)
	if err != nil {
		return Record{}, err
	}
	line, err := json.Marshal(record)
	if err != nil {
		return Record{}, fmt.Errorf("encode record: %w", err)
	}
	line = append(line, '\n')

	file, err := os.OpenFile(j.journalPath(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return Record{}, fmt.Errorf("open journal for append: %w", err)
	}
	written, writeErr := file.Write(line)
	if writeErr == nil && written != len(line) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return Record{}, fmt.Errorf("append journal: %w", writeErr)
	}
	if closeErr != nil {
		return Record{}, fmt.Errorf("close journal: %w", closeErr)
	}

	newHead := headCache{Seq: record.Seq, Hash: record.Hash, Size: info.Size() + int64(len(line))}
	if err := atomicWriteJSON(j.headPath(), newHead, 0o640); err != nil {
		return Record{}, fmt.Errorf("record committed but head cache update failed: %w", err)
	}
	return record, nil
}

func (j *Journal) loadTrustedHead(meta Meta, size int64) (headCache, error) {
	var cached headCache
	if err := readJSONFile(j.headPath(), &cached); err == nil && cached.Size == size {
		if size == 0 {
			anchor, anchorErr := anchorHash(meta)
			if anchorErr == nil && cached.Seq == 0 && cached.Hash == anchor {
				return cached, nil
			}
		} else if record, recordErr := readLastRecord(j.journalPath(), size); recordErr == nil {
			if record.Seq == cached.Seq && record.Hash == cached.Hash {
				if hash, hashErr := hashRecord(record); hashErr == nil && hash == record.Hash {
					return cached, nil
				}
			}
		}
	}

	result, err := j.verifyUnlocked(meta, VerifyOptions{}, nil)
	if err != nil {
		return headCache{}, err
	}
	rebuilt := headCache{Seq: result.Seq, Hash: result.Hash, Size: size}
	if err := atomicWriteJSON(j.headPath(), rebuilt, 0o640); err != nil {
		return headCache{}, fmt.Errorf("rebuild head cache: %w", err)
	}
	return rebuilt, nil
}

func (j *Journal) loadMeta() (Meta, error) {
	var meta Meta
	if err := readJSONFile(j.metaPath(), &meta); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Meta{}, ErrNotInitialized
		}
		return Meta{}, fmt.Errorf("read metadata: %w", err)
	}
	if meta.Version != FormatVersion {
		return Meta{}, fmt.Errorf("unsupported journal format version %d", meta.Version)
	}
	if len(meta.JournalID) != 32 {
		return Meta{}, fmt.Errorf("invalid journal id in metadata")
	}
	if _, err := hex.DecodeString(meta.JournalID); err != nil {
		return Meta{}, fmt.Errorf("invalid journal id in metadata: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, meta.CreatedAt); err != nil {
		return Meta{}, fmt.Errorf("invalid metadata creation time: %w", err)
	}
	return meta, nil
}

func hashRecord(record Record) (string, error) {
	without := recordWithoutHash{
		Seq: record.Seq, Time: record.Time, Kind: record.Kind,
		Payload: record.Payload, Prev: record.Prev,
	}
	raw, err := json.Marshal(without)
	if err != nil {
		return "", fmt.Errorf("encode record for hashing: %w", err)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return "", fmt.Errorf("canonicalize record: %w", err)
	}
	h := sha256.New()
	h.Write([]byte("chaintrail-record-v1\n"))
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func anchorHash(meta Meta) (string, error) {
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte("chaintrail-anchor-v1\n"))
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readLastRecord(path string, size int64) (Record, error) {
	const tailWindow = int64(maxRecordBytes + 1)
	start := int64(0)
	if size > tailWindow {
		start = size - tailWindow
	}
	file, err := os.Open(path)
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return Record{}, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return Record{}, err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return Record{}, integrityf("journal has an incomplete final record")
	}
	data = data[:len(data)-1]
	idx := bytes.LastIndexByte(data, '\n')
	line := data
	if idx >= 0 {
		line = data[idx+1:]
	}
	if start > 0 && idx < 0 {
		return Record{}, fmt.Errorf("final record exceeds maximum supported size")
	}
	return decodeRecord(line)
}

func decodeRecord(line []byte) (Record, error) {
	if len(line) == 0 {
		return Record{}, integrityf("journal contains an empty record")
	}
	if len(line) > maxRecordBytes {
		return Record{}, integrityf("journal record exceeds maximum supported size")
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var record Record
	if err := dec.Decode(&record); err != nil {
		return Record{}, integrityf("malformed journal record: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Record{}, integrityf("malformed journal record: trailing data")
	}
	return record, nil
}

func verifyRecord(record Record, expectedSeq uint64, expectedPrev string) error {
	if record.Seq != expectedSeq {
		return integrityf("sequence mismatch: expected %d, got %d", expectedSeq, record.Seq)
	}
	if record.Prev != expectedPrev {
		return integrityf("record %d previous hash mismatch", record.Seq)
	}
	if !kindPattern.MatchString(record.Kind) {
		return integrityf("record %d has invalid kind", record.Seq)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.Time); err != nil {
		return integrityf("record %d has invalid timestamp", record.Seq)
	}
	canonicalPayload, err := canonicalJSON(record.Payload)
	if err != nil {
		return integrityf("record %d payload is invalid JSON: %v", record.Seq, err)
	}
	if !bytes.Equal(canonicalPayload, record.Payload) {
		return integrityf("record %d payload is not canonical JSON", record.Seq)
	}
	if len(record.Hash) != 64 {
		return integrityf("record %d has invalid hash length", record.Seq)
	}
	if _, err := hex.DecodeString(record.Hash); err != nil {
		return integrityf("record %d has invalid hash encoding", record.Seq)
	}
	expectedHash, err := hashRecord(record)
	if err != nil {
		return err
	}
	if record.Hash != expectedHash {
		return integrityf("record %d hash mismatch", record.Seq)
	}
	return nil
}

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func atomicWriteJSON(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".chaintrail-tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	defer cleanup()
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary file mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync journal directory: %w", err)
	}
	return nil
}

func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

var (
	errIncompleteRecord = errors.New("incomplete final record")
	errRecordTooLarge   = errors.New("record too large")
)

func readRecordLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 4096)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxRecordBytes {
			return nil, errRecordTooLarge
		}
		line = append(line, fragment...)
		switch err {
		case nil:
			return line[:len(line)-1], nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if len(line) == 0 {
				return nil, io.EOF
			}
			return nil, errIncompleteRecord
		default:
			return nil, err
		}
	}
}

func scanRecords(reader io.Reader, meta Meta, visit func(Record) error) (VerifyResult, error) {
	anchor, err := anchorHash(meta)
	if err != nil {
		return VerifyResult{}, err
	}
	result := VerifyResult{JournalID: meta.JournalID, Hash: anchor}
	buffered := bufio.NewReaderSize(reader, 64*1024)
	expectedSeq := uint64(1)
	previous := anchor
	for {
		line, readErr := readRecordLine(buffered)
		if readErr == io.EOF {
			break
		}
		if errors.Is(readErr, errIncompleteRecord) {
			return VerifyResult{}, integrityf("journal has an incomplete final record at sequence %d", expectedSeq)
		}
		if errors.Is(readErr, errRecordTooLarge) {
			return VerifyResult{}, integrityf("record %d exceeds maximum supported size", expectedSeq)
		}
		if readErr != nil {
			return VerifyResult{}, fmt.Errorf("read journal: %w", readErr)
		}
		record, err := decodeRecord(line)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("sequence %d: %w", expectedSeq, err)
		}
		if err := verifyRecord(record, expectedSeq, previous); err != nil {
			return VerifyResult{}, err
		}
		if visit != nil {
			if err := visit(record); err != nil {
				return VerifyResult{}, err
			}
		}
		result.Seq = record.Seq
		result.Hash = record.Hash
		previous = record.Hash
		expectedSeq++
	}
	return result, nil
}
