package plugins

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

func (s *Service) decryptConfigSecret(record configRecord, key string) (string, error) {
	ciphertext := strings.TrimSpace(record.SecretCiphertexts[key])
	if ciphertext == "" {
		return "", nil
	}
	return decryptSecret(s.secretKey, ciphertext)
}

func encryptSecret(secretKey string, value string) (string, error) {
	block, err := aes.NewCipher(secretKeyBytes(secretKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func decryptSecret(secretKey string, encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(secretKeyBytes(secretKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("encrypted plugin secret is invalid")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func secretKeyBytes(secretKey string) []byte {
	sum := sha256.Sum256([]byte(secretKey))
	return sum[:]
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 10 {
		return strings.Repeat("*", len(value))
	}
	return value[:6] + "..." + value[len(value)-4:]
}
