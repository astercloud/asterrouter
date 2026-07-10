package plugins

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/gowebpki/jcs"
)

func verifySignedEnvelope(envelope catalogEnvelope, cfg OfficialCatalogConfig, purpose string, now time.Time) error {
	if envelope.SchemaVersion != catalogEnvelopeSchema || envelope.Purpose != purpose || envelope.KeyID != cfg.PublicKeyID || envelope.Algorithm != "Ed25519" {
		return ErrCatalogSignature
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, envelope.IssuedAt)
	if err != nil || issuedAt.After(now.Add(5*time.Minute)) {
		return ErrCatalogSignature
	}
	if envelope.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, envelope.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			return ErrCatalogSignature
		}
	}
	publicKey, err := base64.StdEncoding.DecodeString(cfg.PublicKeyBase64)
	if err != nil {
		publicKey, err = base64.RawStdEncoding.DecodeString(cfg.PublicKeyBase64)
	}
	if err != nil {
		publicKey, err = base64.RawURLEncoding.DecodeString(cfg.PublicKeyBase64)
	}
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return ErrCatalogSignature
	}
	signature, err := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return ErrCatalogSignature
	}
	message, err := catalogEnvelopeSigningMessage(envelope)
	if err != nil {
		return ErrCatalogSignature
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), message, signature) {
		return ErrCatalogSignature
	}
	return nil
}

func catalogEnvelopeSigningMessage(envelope catalogEnvelope) ([]byte, error) {
	unsigned := struct {
		SchemaVersion string          `json:"schema_version"`
		Purpose       string          `json:"purpose"`
		KeyID         string          `json:"key_id"`
		Algorithm     string          `json:"algorithm"`
		IssuedAt      string          `json:"issued_at"`
		ExpiresAt     string          `json:"expires_at,omitempty"`
		Payload       json.RawMessage `json:"payload"`
	}{
		SchemaVersion: envelope.SchemaVersion,
		Purpose:       envelope.Purpose,
		KeyID:         envelope.KeyID,
		Algorithm:     envelope.Algorithm,
		IssuedAt:      envelope.IssuedAt,
		ExpiresAt:     envelope.ExpiresAt,
		Payload:       envelope.Payload,
	}
	raw, err := json.Marshal(unsigned)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(raw)
}
