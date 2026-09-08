package chaintrail

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignedCheckpointRoundTripAndHistoricalVerification(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("deploy.started", []byte(`{"version":"1.2.3"}`)); err != nil {
		t.Fatal(err)
	}
	keys := t.TempDir()
	privatePath := filepath.Join(keys, "checkpoint.key.pem")
	publicPath := filepath.Join(keys, "checkpoint.pub.pem")
	keyID, err := GenerateSigningKeyPair(privatePath, publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if keyID == "" {
		t.Fatal("expected key id")
	}
	info, err := os.Stat(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode=%o", info.Mode().Perm())
	}

	token, signed, err := j.SignedCheckpoint(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifySignedCheckpoint(token, publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if verified != signed || verified.KeyID != keyID {
		t.Fatalf("signed checkpoint mismatch: %+v vs %+v", verified, signed)
	}

	if _, err := j.Append("deploy.completed", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	checkpoint := verified.Checkpoint()
	result, err := j.Verify(VerifyOptions{Checkpoint: &checkpoint})
	if err != nil {
		t.Fatalf("signed historical checkpoint should remain valid after growth: %v", err)
	}
	if result.Seq != 2 {
		t.Fatalf("expected head seq 2, got %d", result.Seq)
	}
}

func TestSignedCheckpointDetectsTamperedSignature(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("event", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	keys := t.TempDir()
	privatePath := filepath.Join(keys, "key.pem")
	publicPath := filepath.Join(keys, "key.pub.pem")
	if _, err := GenerateSigningKeyPair(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	token, _, err := j.SignedCheckpoint(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	signature[0] ^= 0xff
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	if _, err := VerifySignedCheckpoint(strings.Join(parts, "."), publicPath); err == nil {
		t.Fatal("expected tampered signature to fail")
	}
}

func TestSignedCheckpointRejectsWrongKey(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Append("event", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	one := t.TempDir()
	two := t.TempDir()
	privatePath := filepath.Join(one, "key.pem")
	publicPath := filepath.Join(one, "key.pub.pem")
	wrongPublic := filepath.Join(two, "wrong.pub.pem")
	if _, err := GenerateSigningKeyPair(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateSigningKeyPair(filepath.Join(two, "wrong.key.pem"), wrongPublic); err != nil {
		t.Fatal(err)
	}
	token, _, err := j.SignedCheckpoint(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySignedCheckpoint(token, wrongPublic); err == nil {
		t.Fatal("expected wrong public key to fail")
	}
}

func TestKeyGenerationRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "key.pem")
	publicPath := filepath.Join(dir, "key.pub.pem")
	if _, err := GenerateSigningKeyPair(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateSigningKeyPair(privatePath, publicPath); err == nil {
		t.Fatal("expected overwrite attempt to fail")
	}
	after, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("existing private key changed after refused overwrite")
	}
}
