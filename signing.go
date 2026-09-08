package chaintrail

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const signedCheckpointVersion = 1
const signedCheckpointPrefix = "CT1"

// SignedCheckpoint is the authenticated payload embedded in a signed token.
type SignedCheckpoint struct {
	Version   int    `json:"version"`
	JournalID string `json:"journal_id"`
	Seq       uint64 `json:"seq"`
	Hash      string `json:"hash"`
	SignedAt  string `json:"signed_at"`
	KeyID     string `json:"key_id"`
}

func (s SignedCheckpoint) Checkpoint() Checkpoint {
	return Checkpoint{JournalID: s.JournalID, Seq: s.Seq, Hash: s.Hash}
}

// GenerateSigningKeyPair creates an Ed25519 key pair without overwriting
// existing files. The private key is PKCS#8 PEM with mode 0600; the public key
// is PKIX PEM with mode 0644. The returned key ID is SHA-256(public key).
func GenerateSigningKeyPair(privatePath, publicPath string) (string, error) {
	if privatePath == "" || publicPath == "" {
		return "", fmt.Errorf("private and public key paths are required")
	}
	if filepath.Clean(privatePath) == filepath.Clean(publicPath) {
		return "", fmt.Errorf("private and public key paths must differ")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate Ed25519 key: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", fmt.Errorf("encode private key: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("encode public key: %w", err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})

	if err := writeExclusive(privatePath, privatePEM, 0o600); err != nil {
		return "", fmt.Errorf("write private key: %w", err)
	}
	if err := writeExclusive(publicPath, publicPEM, 0o644); err != nil {
		_ = os.Remove(privatePath)
		return "", fmt.Errorf("write public key: %w", err)
	}
	return signingKeyID(publicKey), nil
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

// SignedCheckpoint creates a signed trust anchor for the current verified head.
func (j *Journal) SignedCheckpoint(privateKeyPath string) (string, SignedCheckpoint, error) {
	checkpoint, err := j.Checkpoint()
	if err != nil {
		return "", SignedCheckpoint{}, err
	}
	privateKey, err := loadPrivateKey(privateKeyPath)
	if err != nil {
		return "", SignedCheckpoint{}, err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	payload := SignedCheckpoint{
		Version:   signedCheckpointVersion,
		JournalID: checkpoint.JournalID,
		Seq:       checkpoint.Seq,
		Hash:      checkpoint.Hash,
		SignedAt:  j.now().UTC().Format(time.RFC3339Nano),
		KeyID:     signingKeyID(publicKey),
	}
	token, err := signCheckpointPayload(payload, privateKey)
	if err != nil {
		return "", SignedCheckpoint{}, err
	}
	return token, payload, nil
}

func signCheckpointPayload(payload SignedCheckpoint, privateKey ed25519.PrivateKey) (string, error) {
	// Typed struct encoding is intentionally used here instead of Chaintrail's
	// record canonicalizer: token fields include uint64 values that should remain
	// ordinary JSON integers for strict typed decoding on verification.
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode signed checkpoint: %w", err)
	}
	signature := ed25519.Sign(privateKey, signedCheckpointMessage(raw))
	return signedCheckpointPrefix + "." + base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// VerifySignedCheckpoint verifies token syntax, deterministic typed encoding,
// key identity, and Ed25519 signature. Call Journal.Verify with
// payload.Checkpoint() to bind it to a concrete journal and replay the chain
// through the signed sequence.
func VerifySignedCheckpoint(token, publicKeyPath string) (SignedCheckpoint, error) {
	publicKey, err := loadPublicKey(publicKeyPath)
	if err != nil {
		return SignedCheckpoint{}, err
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != signedCheckpointPrefix {
		return SignedCheckpoint{}, fmt.Errorf("invalid signed checkpoint token")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return SignedCheckpoint{}, fmt.Errorf("decode signed checkpoint payload: %w", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return SignedCheckpoint{}, fmt.Errorf("decode signed checkpoint signature: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return SignedCheckpoint{}, fmt.Errorf("invalid signed checkpoint signature length")
	}

	dec := json.NewDecoder(bytes.NewReader(payloadBytes))
	dec.DisallowUnknownFields()
	var payload SignedCheckpoint
	if err := dec.Decode(&payload); err != nil {
		return SignedCheckpoint{}, fmt.Errorf("decode signed checkpoint: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return SignedCheckpoint{}, fmt.Errorf("decode signed checkpoint: trailing data")
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return SignedCheckpoint{}, fmt.Errorf("re-encode signed checkpoint: %w", err)
	}
	if !bytes.Equal(normalized, payloadBytes) {
		return SignedCheckpoint{}, fmt.Errorf("signed checkpoint payload is not in deterministic token encoding")
	}
	if payload.Version != signedCheckpointVersion {
		return SignedCheckpoint{}, fmt.Errorf("unsupported signed checkpoint version %d", payload.Version)
	}
	if _, err := ParseCheckpoint(payload.Checkpoint().String()); err != nil {
		return SignedCheckpoint{}, fmt.Errorf("invalid signed checkpoint payload: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.SignedAt); err != nil {
		return SignedCheckpoint{}, fmt.Errorf("invalid signed checkpoint time: %w", err)
	}
	expectedKeyID := signingKeyID(publicKey)
	if payload.KeyID != expectedKeyID {
		return SignedCheckpoint{}, fmt.Errorf("signed checkpoint key id mismatch")
	}
	if !ed25519.Verify(publicKey, signedCheckpointMessage(payloadBytes), signature) {
		return SignedCheckpoint{}, fmt.Errorf("signed checkpoint signature verification failed")
	}
	return payload, nil
}

func signedCheckpointMessage(payload []byte) []byte {
	message := make([]byte, 0, len(payload)+32)
	message = append(message, []byte("chaintrail-checkpoint-signature-v1\n")...)
	message = append(message, payload...)
	return message
}

func signingKeyID(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return hex.EncodeToString(digest[:])
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("private key must contain exactly one PKCS#8 PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not Ed25519")
	}
	return key, nil
}

func loadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("public key must contain exactly one PKIX PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not Ed25519")
	}
	return key, nil
}
