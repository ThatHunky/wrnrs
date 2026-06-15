package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

type AnswerCipher struct {
	aead cipher.AEAD
}

func NewAnswerCipher(key []byte) (*AnswerCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("answer encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create answer cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create answer gcm: %w", err)
	}
	return &AnswerCipher{aead: aead}, nil
}

func (c *AnswerCipher) Encrypt(plaintext string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("answer cipher is not configured")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate answer nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	return append(nonce, sealed...), nil
}

func (c *AnswerCipher) Decrypt(ciphertext []byte) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("answer cipher is not configured")
	}
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("encrypted answer is too short")
	}
	nonce := ciphertext[:nonceSize]
	sealed := ciphertext[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt answer: %w", err)
	}
	return string(plaintext), nil
}
