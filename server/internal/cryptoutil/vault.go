package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const version = "v1"

type Vault struct{ key [sha256.Size]byte }

func NewVault(secret string) *Vault {
	return &Vault{key: sha256.Sum256([]byte(secret))}
}

func (v *Vault) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil)
	tagOffset := len(sealed) - gcm.Overhead()
	ciphertext, tag := sealed[:tagOffset], sealed[tagOffset:]
	encode := base64.RawURLEncoding.EncodeToString
	return strings.Join([]string{version, encode(iv), encode(tag), encode(ciphertext)}, ":"), nil
}

func (v *Vault) Decrypt(serialized string) (string, error) {
	parts := strings.Split(serialized, ":")
	if len(parts) != 4 || parts[0] != version {
		return "", fmt.Errorf("unsupported encrypted credential format")
	}
	decode := base64.RawURLEncoding.DecodeString
	iv, err := decode(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	tag, err := decode(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode tag: %w", err)
	}
	ciphertext, err := decode(parts[3])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	if len(iv) != gcm.NonceSize() || len(tag) != gcm.Overhead() {
		return "", fmt.Errorf("invalid encrypted credential lengths")
	}
	plaintext, err := gcm.Open(nil, iv, append(ciphertext, tag...), nil)
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plaintext), nil
}

func TokensEqual(candidate, expected string) bool {
	a := sha256.Sum256([]byte(candidate))
	b := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
