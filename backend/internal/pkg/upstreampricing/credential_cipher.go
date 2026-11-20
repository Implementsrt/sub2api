package upstreampricing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const pricingCredentialPrefix = "pricing-v1:"

type credentialCipher struct {
	aead   cipher.AEAD
	legacy Cipher
}

// Protect new credentials with the persisted bootstrap secret. The fallback
// reads older credentials on installations that used a fixed encryption key.
func NewCredentialCipher(secret string, legacy Cipher) (Cipher, error) {
	if secret == "" {
		return nil, errors.New("upstream pricing requires a persisted application secret")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("sub2api/upstream-pricing-credentials/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &credentialCipher{aead: aead, legacy: legacy}, nil
}

func (c *credentialCipher) Encrypt(value string) (string, error) {
	sealed := c.aead.Seal(nil, nil, []byte(value), nil)
	return pricingCredentialPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *credentialCipher) Decrypt(value string) (string, error) {
	encoded, ok := strings.CutPrefix(value, pricingCredentialPrefix)
	if !ok {
		if c.legacy == nil {
			return "", errors.New("legacy upstream pricing credential cannot be decrypted")
		}
		return c.legacy.Decrypt(value)
	}
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	plaintext, err := c.aead.Open(nil, nil, sealed, nil)
	return string(plaintext), err
}
