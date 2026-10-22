package calendar

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

type Sealer struct {
	aead cipher.AEAD
}

func NewSealer(encodedKey string) (*Sealer, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("calendar: token key is not base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("calendar: token key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

func (s *Sealer) Seal(plain string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (s *Sealer) Open(sealed []byte) (string, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size {
		return "", errors.New("calendar: sealed token is too short")
	}
	plain, err := s.aead.Open(nil, sealed[:size], sealed[size:], nil)
	if err != nil {
		return "", fmt.Errorf("calendar: cannot open sealed token: %w", err)
	}
	return string(plain), nil
}
