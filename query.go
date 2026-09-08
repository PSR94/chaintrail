package chaintrail

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// QueryOptions selects records while still validating the complete journal.
// Zero values mean "no filter" except Limit, where zero means unlimited.
type QueryOptions struct {
	Kind       string
	KindPrefix string
	FromSeq    uint64
	ToSeq      uint64
	Since      *time.Time
	Until      *time.Time
	Limit      int
}

// QueryResult includes the verified journal head alongside matching records so
// callers can tie a query result to the exact chain state that was inspected.
type QueryResult struct {
	JournalID string   `json:"journal_id"`
	HeadSeq   uint64   `json:"head_seq"`
	HeadHash  string   `json:"head_hash"`
	Records   []Record `json:"records"`
}

func (options QueryOptions) validate() error {
	if options.Kind != "" && options.KindPrefix != "" {
		return fmt.Errorf("query kind and kind-prefix are mutually exclusive")
	}
	if options.Kind != "" && !kindPattern.MatchString(options.Kind) {
		return fmt.Errorf("invalid query kind %q", options.Kind)
	}
	if options.KindPrefix != "" && len(options.KindPrefix) > 64 {
		return fmt.Errorf("query kind-prefix exceeds 64 characters")
	}
	if options.ToSeq != 0 && options.FromSeq != 0 && options.FromSeq > options.ToSeq {
		return fmt.Errorf("query from sequence must not exceed to sequence")
	}
	if options.Since != nil && options.Until != nil && options.Since.After(*options.Until) {
		return fmt.Errorf("query since time must not be after until time")
	}
	if options.Limit < 0 {
		return fmt.Errorf("query limit must be non-negative")
	}
	return nil
}

// Query validates the entire journal before returning filtered records. It does
// not stop integrity verification when Limit is reached.
func (j *Journal) Query(options QueryOptions) (QueryResult, error) {
	if err := options.validate(); err != nil {
		return QueryResult{}, err
	}
	meta, err := j.loadMeta()
	if err != nil {
		return QueryResult{}, err
	}
	lock, err := acquireFileLock(j.lockPath(), false)
	if err != nil {
		return QueryResult{}, err
	}
	defer lock.Close()

	file, err := os.Open(j.journalPath())
	if err != nil {
		return QueryResult{}, fmt.Errorf("open journal: %w", err)
	}
	defer file.Close()

	matches := make([]Record, 0)
	result, err := scanRecords(file, meta, func(record Record) error {
		if options.FromSeq != 0 && record.Seq < options.FromSeq {
			return nil
		}
		if options.ToSeq != 0 && record.Seq > options.ToSeq {
			return nil
		}
		if options.Kind != "" && record.Kind != options.Kind {
			return nil
		}
		if options.KindPrefix != "" && !strings.HasPrefix(record.Kind, options.KindPrefix) {
			return nil
		}
		if options.Since != nil || options.Until != nil {
			at, parseErr := time.Parse(time.RFC3339Nano, record.Time)
			if parseErr != nil {
				return integrityf("record %d has invalid timestamp", record.Seq)
			}
			if options.Since != nil && at.Before(*options.Since) {
				return nil
			}
			if options.Until != nil && at.After(*options.Until) {
				return nil
			}
		}
		if options.Limit == 0 || len(matches) < options.Limit {
			matches = append(matches, record)
		}
		return nil
	})
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{
		JournalID: result.JournalID,
		HeadSeq:   result.Seq,
		HeadHash:  result.Hash,
		Records:   matches,
	}, nil
}
