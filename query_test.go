package chaintrail

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestQueryFiltersRecordsAndReturnsVerifiedHead(t *testing.T) {
	j := newTestJournal(t)
	for _, item := range []struct {
		kind    string
		payload string
	}{
		{"deploy.started", `{"service":"api"}`},
		{"job.started", `{"job":"backup"}`},
		{"deploy.completed", `{"service":"api"}`},
		{"job.completed", `{"job":"backup"}`},
	} {
		if _, err := j.Append(item.kind, []byte(item.payload)); err != nil {
			t.Fatal(err)
		}
	}

	result, err := j.Query(QueryOptions{KindPrefix: "deploy.", FromSeq: 2, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.HeadSeq != 4 || len(result.Records) != 1 || result.Records[0].Seq != 3 {
		t.Fatalf("unexpected query result: %+v", result)
	}
	if result.HeadHash == "" || result.JournalID == "" {
		t.Fatal("query result must identify verified journal head")
	}
}

func TestQueryTimeRangeIsInclusive(t *testing.T) {
	j := newTestJournal(t)
	for i := 0; i < 3; i++ {
		if _, err := j.Append("event", []byte(`{"ok":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Date(2026, 9, 8, 12, 0, 2, 0, time.UTC)
	until := time.Date(2026, 9, 8, 12, 0, 3, 0, time.UTC)
	result, err := j.Query(QueryOptions{Since: &since, Until: &until})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || result.Records[0].Seq != 2 || result.Records[1].Seq != 3 {
		t.Fatalf("unexpected time query: %+v", result.Records)
	}
}

func TestQueryStillVerifiesAfterLimitReached(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("match", []byte(`{"value":"safe"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append("later", []byte(`{"value":"alpha"}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(j.journalPath())
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "alpha", "bravo", 1))
	if err := os.WriteFile(j.journalPath(), data, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Query(QueryOptions{Kind: "match", Limit: 1}); !IsIntegrityError(err) {
		t.Fatalf("expected integrity error after limited match, got %v", err)
	}
}

func TestQueryValidation(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Query(QueryOptions{Kind: "a", KindPrefix: "a."}); err == nil {
		t.Fatal("expected mutually exclusive kind filter error")
	}
	if _, err := j.Query(QueryOptions{FromSeq: 10, ToSeq: 2}); err == nil {
		t.Fatal("expected invalid sequence range error")
	}
	if _, err := j.Query(QueryOptions{Limit: -1}); err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestStatsAreDerivedFromVerifiedJournal(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("deploy", []byte(`{"service":"api"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append("deploy", []byte(`{"service":"worker"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append("backup", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	stats, err := j.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.HeadSeq != 3 || stats.DistinctKinds != 2 || stats.RecordsByKind["deploy"] != 2 || stats.RecordsByKind["backup"] != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.JournalBytes == 0 || stats.PayloadBytes == 0 || stats.FirstRecordTime == "" || stats.LastRecordTime == "" {
		t.Fatalf("stats missing operational fields: %+v", stats)
	}
}

func TestStatsRejectTamperedJournal(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("event", []byte(`{"value":"alpha"}`)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(j.journalPath())
	data = []byte(strings.Replace(string(data), "alpha", "bravo", 1))
	if err := os.WriteFile(j.journalPath(), data, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Stats(); !IsIntegrityError(err) {
		t.Fatalf("expected integrity error, got %v", err)
	}
}
