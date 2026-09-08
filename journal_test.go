package chaintrail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestJournal(t *testing.T) *Journal {
	t.Helper()
	j := Open(filepath.Join(t.TempDir(), "journal"))
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	step := 0
	j.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		v := base.Add(time.Duration(step) * time.Second)
		step++
		return v
	}
	if _, err := j.Init(); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestCanonicalJSONNormalizesOrderingAndNumbers(t *testing.T) {
	one, err := canonicalJSON([]byte(`{"b":1.00,"a":[0.010,100]}`))
	if err != nil {
		t.Fatal(err)
	}
	two, err := canonicalJSON([]byte(` { "a" : [1e-2,1e2], "b": 1 } `))
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatalf("canonical forms differ:\n%s\n%s", one, two)
	}
}

func TestAppendVerifyAndCheckpointPrefix(t *testing.T) {
	j := newTestJournal(t)
	first, err := j.Append("deploy.started", []byte(`{"version":"1.2.3","attempt":1.0}`))
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := j.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Seq != first.Seq || checkpoint.Hash != first.Hash {
		t.Fatal("checkpoint mismatch")
	}
	if _, err := j.Append("deploy.completed", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	result, err := j.Verify(VerifyOptions{Checkpoint: &checkpoint})
	if err != nil {
		t.Fatalf("historical checkpoint should verify after journal growth: %v", err)
	}
	if result.Seq != 2 {
		t.Fatalf("expected seq 2, got %d", result.Seq)
	}
}

func TestTamperDetection(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("event.one", []byte(`{"value":"alpha"}`)); err != nil {
		t.Fatal(err)
	}
	path := j.journalPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "alpha", "bravo", 1))
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Verify(VerifyOptions{}); !IsIntegrityError(err) {
		t.Fatalf("expected integrity error, got %v", err)
	}
}

func TestStaleHeadCacheRebuiltBeforeAppend(t *testing.T) {
	j := newTestJournal(t)
	first, err := j.Append("event.one", []byte(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j.headPath(), []byte(`{"seq":0,"hash":"bad","size":0}\n`), 0o640); err != nil {
		t.Fatal(err)
	}
	second, err := j.Append("event.two", []byte(`{"n":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if second.Seq != 2 || second.Prev != first.Hash {
		t.Fatalf("head was not rebuilt: %+v", second)
	}
}

func TestConcurrentAppendsSerialize(t *testing.T) {
	j := newTestJournal(t)
	const writers = 8
	const each = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for worker := 0; worker < writers; worker++ {
		for item := 0; item < each; item++ {
			wg.Add(1)
			go func(worker, item int) {
				defer wg.Done()
				payload := []byte(fmt.Sprintf(`{"worker":%d,"item":%d}`, worker, item))
				_, err := j.Append("concurrent.event", payload)
				errs <- err
			}(worker, item)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := j.Verify(VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Seq != writers*each {
		t.Fatalf("expected %d records, got %d", writers*each, result.Seq)
	}
}

func TestRepairTruncatesOnlyPartialTail(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("event.one", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(j.journalPath(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"seq":2,"time":"partial`); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := j.Verify(VerifyOptions{}); !IsIntegrityError(err) {
		t.Fatalf("expected partial-tail integrity error, got %v", err)
	}
	repair, err := j.Repair()
	if err != nil {
		t.Fatal(err)
	}
	if !repair.Repaired || repair.RemovedBytes == 0 {
		t.Fatalf("unexpected repair result: %+v", repair)
	}
	result, err := j.Verify(VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Seq != 1 {
		t.Fatalf("expected one intact record, got %d", result.Seq)
	}
}

func TestRepairRefusesEarlierCorruption(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("event.one", []byte(`{"value":"alpha"}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(j.journalPath())
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "alpha", "bravo", 1) + `{"partial":`)
	if err := os.WriteFile(j.journalPath(), data, 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(j.journalPath())
	if _, err := j.Repair(); !IsIntegrityError(err) {
		t.Fatalf("expected integrity error, got %v", err)
	}
	after, _ := os.ReadFile(j.journalPath())
	if string(before) != string(after) {
		t.Fatal("repair modified journal despite earlier corruption")
	}
}

func TestTailValidatesBeforeReturning(t *testing.T) {
	j := newTestJournal(t)
	for i := 1; i <= 4; i++ {
		if _, err := j.Append("event", []byte(fmt.Sprintf(`{"n":%d}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	records, err := j.Tail(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Seq != 3 || records[1].Seq != 4 {
		t.Fatalf("unexpected tail: %+v", records)
	}
	data, _ := os.ReadFile(j.journalPath())
	var rec Record
	lineEnd := strings.IndexByte(string(data), '\n')
	if err := json.Unmarshal(data[:lineEnd], &rec); err != nil {
		t.Fatal(err)
	}
	rec.Hash = strings.Repeat("0", 64)
	bad, _ := json.Marshal(rec)
	tampered := append(append([]byte{}, bad...), data[lineEnd:]...)
	if err := os.WriteFile(j.journalPath(), tampered, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Tail(1); !IsIntegrityError(err) {
		t.Fatalf("expected integrity error, got %v", err)
	}
}

func TestCheckpointParsing(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("event", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := j.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCheckpoint(checkpoint.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != checkpoint {
		t.Fatalf("round trip mismatch: %+v vs %+v", parsed, checkpoint)
	}
	if _, err := ParseCheckpoint("not-a-checkpoint"); err == nil {
		t.Fatal("expected parse error")
	}
}
