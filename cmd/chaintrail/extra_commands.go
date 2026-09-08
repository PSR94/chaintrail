package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/PSR94/chaintrail"
)

func parseOptionalTime(value, name string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be RFC3339/RFC3339Nano: %v", errUsage, name, err)
	}
	return &parsed, nil
}

func runQuery(args []string, stdout, stderr io.Writer) error {
	set := flags("query", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	kind := set.String("kind", "", "exact record kind")
	kindPrefix := set.String("kind-prefix", "", "record kind prefix")
	from := set.Uint64("from", 0, "minimum sequence, inclusive")
	to := set.Uint64("to", 0, "maximum sequence, inclusive")
	sinceText := set.String("since", "", "minimum timestamp, inclusive (RFC3339)")
	untilText := set.String("until", "", "maximum timestamp, inclusive (RFC3339)")
	limit := set.Int("limit", 0, "maximum matching records; 0 means unlimited")
	format := set.String("format", "ndjson", "output format: ndjson or json")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	if *format != "ndjson" && *format != "json" {
		return fmt.Errorf("%w: -format must be ndjson or json", errUsage)
	}
	since, err := parseOptionalTime(*sinceText, "-since")
	if err != nil {
		return err
	}
	until, err := parseOptionalTime(*untilText, "-until")
	if err != nil {
		return err
	}
	result, err := chaintrail.Open(*dir).Query(chaintrail.QueryOptions{
		Kind: *kind, KindPrefix: *kindPrefix, FromSeq: *from, ToSeq: *to,
		Since: since, Until: until, Limit: *limit,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	if *format == "json" {
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	for _, record := range result.Records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return nil
}

func runStats(args []string, stdout, stderr io.Writer) error {
	set := flags("stats", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	format := set.String("format", "text", "output format: text or json")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("%w: -format must be text or json", errUsage)
	}
	stats, err := chaintrail.Open(*dir).Stats()
	if err != nil {
		return err
	}
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(stats)
	}
	fmt.Fprintf(stdout, "journal=%s\nhead_seq=%d\nhead_hash=%s\njournal_bytes=%d\npayload_bytes=%d\ndistinct_kinds=%d\n",
		stats.JournalID, stats.HeadSeq, stats.HeadHash, stats.JournalBytes, stats.PayloadBytes, stats.DistinctKinds)
	if stats.FirstRecordTime != "" {
		fmt.Fprintf(stdout, "first_record_time=%s\nlast_record_time=%s\n", stats.FirstRecordTime, stats.LastRecordTime)
	}
	kinds := make([]string, 0, len(stats.RecordsByKind))
	for kind := range stats.RecordsByKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		fmt.Fprintf(stdout, "kind[%s]=%d\n", kind, stats.RecordsByKind[kind])
	}
	return nil
}

func runKeygen(args []string, stdout, stderr io.Writer) error {
	set := flags("keygen", stderr)
	privatePath := set.String("private", "chaintrail-checkpoint.key.pem", "private Ed25519 key path")
	publicPath := set.String("public", "chaintrail-checkpoint.pub.pem", "public Ed25519 key path")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	keyID, err := chaintrail.GenerateSigningKeyPair(*privatePath, *publicPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "key_id=%s\nprivate=%s\npublic=%s\n", keyID, *privatePath, *publicPath)
	return nil
}
