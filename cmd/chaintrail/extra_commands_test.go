package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIQueryStatsAndSignedCheckpoint(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "journal")
	privatePath := filepath.Join(root, "keys", "checkpoint.key.pem")
	publicPath := filepath.Join(root, "keys", "checkpoint.pub.pem")
	var out, errOut bytes.Buffer

	mustRun := func(args ...string) string {
		t.Helper()
		out.Reset()
		errOut.Reset()
		if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("%v exit=%d stderr=%s", args, code, errOut.String())
		}
		return strings.TrimSpace(out.String())
	}

	mustRun("init", "-dir", dir)
	mustRun("append", "-dir", dir, "-kind", "deploy.started", "-data", `{"service":"api"}`)
	mustRun("append", "-dir", dir, "-kind", "job.started", "-data", `{"job":"backup"}`)

	query := mustRun("query", "-dir", dir, "-kind-prefix", "deploy.", "-format", "json")
	if !strings.Contains(query, `"head_seq": 2`) || !strings.Contains(query, `"kind": "deploy.started"`) {
		t.Fatalf("unexpected query output: %s", query)
	}
	stats := mustRun("stats", "-dir", dir)
	if !strings.Contains(stats, "head_seq=2") || !strings.Contains(stats, "kind[deploy.started]=1") {
		t.Fatalf("unexpected stats output: %s", stats)
	}

	keygen := mustRun("keygen", "-private", privatePath, "-public", publicPath)
	if !strings.Contains(keygen, "key_id=") {
		t.Fatalf("unexpected keygen output: %s", keygen)
	}
	token := mustRun("checkpoint", "-dir", dir, "-sign-key", privatePath)
	if !strings.HasPrefix(token, "CT1.") {
		t.Fatalf("unexpected signed checkpoint: %s", token)
	}
	mustRun("append", "-dir", dir, "-kind", "deploy.completed", "-data", `{"ok":true}`)
	verified := mustRun("verify", "-dir", dir, "-signed-checkpoint", token, "-public-key", publicPath)
	if !strings.Contains(verified, "OK seq=3") {
		t.Fatalf("unexpected verify output: %s", verified)
	}
}

func TestCLISignedCheckpointRequiresPublicKey(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"verify", "-signed-checkpoint", "CT1.invalid.invalid"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("expected usage exit 2, got %d stderr=%s", code, errOut.String())
	}
}
