package chaintrail

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type Checkpoint struct {
	JournalID string `json:"journal_id"`
	Seq       uint64 `json:"seq"`
	Hash      string `json:"hash"`
}

func (c Checkpoint) String() string {
	return fmt.Sprintf("%s:%d:%s", c.JournalID, c.Seq, c.Hash)
}

func ParseCheckpoint(value string) (Checkpoint, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return Checkpoint{}, fmt.Errorf("invalid checkpoint: expected journal_id:sequence:hash")
	}
	if len(parts[0]) != 32 {
		return Checkpoint{}, fmt.Errorf("invalid checkpoint journal id")
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return Checkpoint{}, fmt.Errorf("invalid checkpoint journal id: %w", err)
	}
	seq, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("invalid checkpoint sequence: %w", err)
	}
	if len(parts[2]) != 64 {
		return Checkpoint{}, fmt.Errorf("invalid checkpoint hash")
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return Checkpoint{}, fmt.Errorf("invalid checkpoint hash: %w", err)
	}
	return Checkpoint{JournalID: parts[0], Seq: seq, Hash: parts[2]}, nil
}

func (j *Journal) Checkpoint() (Checkpoint, error) {
	result, err := j.Verify(VerifyOptions{})
	if err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{JournalID: result.JournalID, Seq: result.Seq, Hash: result.Hash}, nil
}
