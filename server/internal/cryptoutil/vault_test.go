package cryptoutil

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type compatibilityFixture struct {
	AES256GCM struct{ SecretUTF8, PlaintextUTF8, Serialized string } `json:"aes256Gcm"`
}

func TestCompatibilityVector(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "tests", "fixtures", "compatibility-vectors.json"))
	if err != nil {
		t.Fatalf("read compatibility fixture: %v", err)
	}
	var fixture compatibilityFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatalf("decode compatibility fixture: %v", err)
	}
	plaintext, err := NewVault(fixture.AES256GCM.SecretUTF8).Decrypt(fixture.AES256GCM.Serialized)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != fixture.AES256GCM.PlaintextUTF8 {
		t.Fatalf("Decrypt() = %q, want %q", plaintext, fixture.AES256GCM.PlaintextUTF8)
	}
}

func TestEncryptRoundTripAndTamper(t *testing.T) {
	vault := NewVault("test-app-secret-that-is-at-least-32-characters")
	first, err := vault.Encrypt("station-token")
	if err != nil {
		t.Fatal(err)
	}
	second, err := vault.Encrypt("station-token")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("Encrypt() reused a nonce")
	}
	plaintext, err := vault.Decrypt(first)
	if err != nil || plaintext != "station-token" {
		t.Fatalf("round trip = %q, %v", plaintext, err)
	}
	parts := strings.Split(first, ":")
	ciphertext, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[0] ^= 1
	parts[3] = base64.RawURLEncoding.EncodeToString(ciphertext)
	tampered := strings.Join(parts, ":")
	if _, err := vault.Decrypt(tampered); err == nil {
		t.Fatal("Decrypt() accepted tampered ciphertext")
	}
}
