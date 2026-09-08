package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/PSR94/chaintrail"
)

const usageText = `Usage:
  chaintrail init [-dir DIR]
  chaintrail append [-dir DIR] -kind KIND (-data JSON | -file PATH | -stdin)
  chaintrail verify [-dir DIR] [-expect HASH] [-checkpoint TOKEN]
  chaintrail checkpoint [-dir DIR]
  chaintrail tail [-dir DIR] [-n COUNT]
  chaintrail repair [-dir DIR]
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	var err error
	switch args[0] {
	case "init":
		err = runInit(args[1:], stdout, stderr)
	case "append":
		err = runAppend(args[1:], stdin, stdout, stderr)
	case "verify":
		err = runVerify(args[1:], stdout, stderr)
	case "checkpoint":
		err = runCheckpoint(args[1:], stdout, stderr)
	case "tail":
		err = runTail(args[1:], stdout, stderr)
	case "repair":
		err = runRepair(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usageText)
		return 2
	}
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, "chaintrail:", err)
	if chaintrail.IsIntegrityError(err) {
		return 3
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if errors.Is(err, errUsage) {
		return 2
	}
	return 1
}

var errUsage = errors.New("invalid usage")

func flags(name string, stderr io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(stderr)
	return set
}

func ensureNoPositionals(set *flag.FlagSet) error {
	if set.NArg() != 0 {
		return fmt.Errorf("%w: unexpected arguments: %v", errUsage, set.Args())
	}
	return nil
}

func runInit(args []string, stdout, stderr io.Writer) error {
	set := flags("init", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	meta, err := chaintrail.Open(*dir).Init()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "initialized %s at %s\n", meta.JournalID, *dir)
	return nil
}

func runAppend(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	set := flags("append", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	kind := set.String("kind", "", "record kind")
	data := set.String("data", "", "inline JSON payload")
	filePath := set.String("file", "", "JSON payload file")
	useStdin := set.Bool("stdin", false, "read JSON payload from stdin")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	if *kind == "" {
		return fmt.Errorf("%w: -kind is required", errUsage)
	}
	sources := 0
	if *data != "" {
		sources++
	}
	if *filePath != "" {
		sources++
	}
	if *useStdin {
		sources++
	}
	if sources != 1 {
		return fmt.Errorf("%w: choose exactly one of -data, -file, or -stdin", errUsage)
	}
	var payload []byte
	var err error
	switch {
	case *data != "":
		payload = []byte(*data)
	case *filePath != "":
		payload, err = os.ReadFile(*filePath)
	default:
		payload, err = io.ReadAll(io.LimitReader(stdin, chaintrail.MaxPayloadBytes+1))
	}
	if err != nil {
		return fmt.Errorf("read payload: %w", err)
	}
	record, err := chaintrail.Open(*dir).Append(*kind, payload)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(record)
	fmt.Fprintln(stdout, string(encoded))
	return nil
}

func runVerify(args []string, stdout, stderr io.Writer) error {
	set := flags("verify", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	expect := set.String("expect", "", "expected current head hash")
	checkpointText := set.String("checkpoint", "", "trusted checkpoint token")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	options := chaintrail.VerifyOptions{ExpectHash: *expect}
	if *checkpointText != "" {
		checkpoint, err := chaintrail.ParseCheckpoint(*checkpointText)
		if err != nil {
			return fmt.Errorf("%w: %v", errUsage, err)
		}
		options.Checkpoint = &checkpoint
	}
	result, err := chaintrail.Open(*dir).Verify(options)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "OK seq=%d hash=%s journal=%s\n", result.Seq, result.Hash, result.JournalID)
	return nil
}

func runCheckpoint(args []string, stdout, stderr io.Writer) error {
	set := flags("checkpoint", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	checkpoint, err := chaintrail.Open(*dir).Checkpoint()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, checkpoint.String())
	return nil
}

func runTail(args []string, stdout, stderr io.Writer) error {
	set := flags("tail", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	count := set.Int("n", 10, "number of records")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	records, err := chaintrail.Open(*dir).Tail(*count)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(stdout)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return err
		}
	}
	return nil
}

func runRepair(args []string, stdout, stderr io.Writer) error {
	set := flags("repair", stderr)
	dir := set.String("dir", ".chaintrail", "journal directory")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if err := ensureNoPositionals(set); err != nil {
		return err
	}
	result, err := chaintrail.Open(*dir).Repair()
	if err != nil {
		return err
	}
	if result.Repaired {
		fmt.Fprintf(stdout, "repaired removed_bytes=%d seq=%d hash=%s\n", result.RemovedBytes, result.Seq, result.Hash)
	} else {
		fmt.Fprintf(stdout, "no repair needed seq=%d hash=%s\n", result.Seq, result.Hash)
	}
	return nil
}
