package settings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/cryptoutil"
)

const (
	settingCiphertextPrefix = "enc:v1:"
	settingCipherPurpose    = "asterrouter:settings:secret-encryption:v1"
)

var encryptedSettingKeys = map[string]struct{}{
	KeyFeishuAppSecret:      {},
	KeyGitHubOAuthSecret:    {},
	KeyGoogleOAuthSecret:    {},
	KeyDingTalkClientSecret: {},
	KeyTurnstileSecretKey:   {},
	KeySMTPPassword:         {},
	KeyBackupS3SecretKey:    {},
}

func (s *Service) readValues(ctx context.Context) (map[string]string, error) {
	values, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	for key := range encryptedSettingKeys {
		value, decryptErr := decryptSettingValue(s.secretKey, key, values[key])
		if decryptErr != nil {
			return nil, fmt.Errorf("decrypt setting %s: %w", key, decryptErr)
		}
		values[key] = value
	}
	return values, nil
}

func (s *Service) encryptValues(values map[string]string) (map[string]string, error) {
	sealed := make(map[string]string, len(values))
	for key, value := range values {
		sealed[key] = value
	}
	for key := range encryptedSettingKeys {
		value, ok := sealed[key]
		if !ok {
			continue
		}
		ciphertext, err := encryptSettingValue(s.secretKey, key, value)
		if err != nil {
			return nil, fmt.Errorf("encrypt setting %s: %w", key, err)
		}
		sealed[key] = ciphertext
	}
	return sealed, nil
}

func encryptSettingValue(secretKey, settingKey, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return value, nil
	}
	key, err := cryptoutil.DeriveKey(secretKey, settingCipherPurpose)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
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
	sealed := gcm.Seal(nonce, nonce, []byte(value), []byte(settingKey))
	return settingCiphertextPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptSettingValue(secretKey, settingKey, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, settingCiphertextPrefix) {
		return value, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, settingCiphertextPrefix))
	if err != nil {
		return "", err
	}
	key, err := cryptoutil.DeriveKey(secretKey, settingCipherPurpose)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted setting")
	}
	plaintext, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(settingKey))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
