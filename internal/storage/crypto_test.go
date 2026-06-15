package storage_test

import (
	"bytes"
	"testing"

	"wrnrs/internal/storage"
)

func TestAnswerCipherEncryptsAndDecryptsWithoutPlaintextLeak(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	cipher, err := storage.NewAnswerCipher(key)
	if err != nil {
		t.Fatalf("NewAnswerCipher returned error: %v", err)
	}

	plaintext := "I feel loved when you listen."
	encrypted, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}
	if bytes.Contains(encrypted, []byte(plaintext)) {
		t.Fatalf("ciphertext %q contains plaintext %q", encrypted, plaintext)
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestAnswerCipherRejectsInvalidKeyLength(t *testing.T) {
	if _, err := storage.NewAnswerCipher([]byte("too-short")); err == nil {
		t.Fatal("NewAnswerCipher succeeded with an invalid key length")
	}
}
