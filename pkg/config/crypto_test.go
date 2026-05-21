package config

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	plaintext := []byte("secret bank data 12345")
	password := "my-secure-password"

	// 1. Encrypt
	envelopeBytes, err := Encrypt(plaintext, password)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	// Verify it detects as encrypted
	if !IsEncrypted(envelopeBytes) {
		t.Error("expected IsEncrypted to return true")
	}

	// 2. Decrypt with correct password
	decrypted, err := Decrypt(envelopeBytes, password)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("expected decrypted to match original plaintext, got %s", string(decrypted))
	}

	// 3. Decrypt with incorrect password should fail
	_, err = Decrypt(envelopeBytes, "wrong-password")
	if err == nil {
		t.Error("expected decryption to fail with incorrect password")
	}
}

func TestIsEncrypted(t *testing.T) {
	plainJSON := []byte(`{"client_id": "test", "secret": "xyz"}`)
	if IsEncrypted(plainJSON) {
		t.Error("expected plain JSON to not be detected as encrypted")
	}

	badData := []byte(`invalid json`)
	if IsEncrypted(badData) {
		t.Error("expected invalid JSON to not be detected as encrypted")
	}
}
