package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLISmokeAndIntegrityExit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "-dir", dir}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("init exit=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"append", "-dir", dir, "-kind", "job.started", "-data", `{"job":"backup"}`}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("append exit=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"checkpoint", "-dir", dir}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("checkpoint exit=%d stderr=%s", code, errOut.String())
	}
	checkpoint := strings.TrimSpace(out.String())
	out.Reset()
	errOut.Reset()
	if code := run([]string{"append", "-dir", dir, "-kind", "job.done", "-data", `{"ok":true}`}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("append 2 exit=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"verify", "-dir", dir, "-checkpoint", checkpoint}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("verify exit=%d stderr=%s", code, errOut.String())
	}
}

func TestCLIRejectsAmbiguousPayloadSource(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"append", "-kind", "x", "-data", `{}`, "-stdin"}, strings.NewReader(`{}`), &out, &errOut)
	if code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}
