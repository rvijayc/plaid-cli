package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	pbkdf2Iterations = 100000
	keySize          = 32 // 256 bits
	saltSize         = 16 // 128 bits
)

// EncryptedEnvelope holds the encryption metadata and the ciphertext.
type EncryptedEnvelope struct {
	Encrypted  bool   `json:"encrypted"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// DeriveKey generates a 256-bit AES key from a password and salt using PBKDF2-SHA256.
func DeriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, keySize, sha256.New)
}

// Encrypt encrypts the plaintext using AES-256-GCM with a key derived from the password.
// It returns the JSON-serialized EncryptedEnvelope.
func Encrypt(plaintext []byte, password string) ([]byte, error) {
	// 1. Generate a random salt
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate random salt: %w", err)
	}

	// 2. Derive key from password and salt
	key := DeriveKey(password, salt)

	// 3. Create AES cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// 4. Create GCM cipher mode
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM mode: %w", err)
	}

	// 5. Generate a random nonce
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}

	// 6. Encrypt the data
	ciphertext := aesGCM.Seal(nil, nonce, plaintext, nil)

	// 7. Construct and serialize the envelope
	envelope := EncryptedEnvelope{
		Encrypted:  true,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}

	envelopeBytes, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal encrypted envelope: %w", err)
	}

	return envelopeBytes, nil
}

// Decrypt decrypts the data envelope using AES-256-GCM with a key derived from the password.
func Decrypt(envelopeBytes []byte, password string) ([]byte, error) {
	// 1. Parse the envelope
	var envelope EncryptedEnvelope
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse encrypted envelope: %w", err)
	}

	if !envelope.Encrypted {
		return nil, errors.New("data is not marked as encrypted")
	}

	// 2. Decode base64 strings
	salt, err := base64.StdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return nil, fmt.Errorf("failed to decode salt: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// 3. Derive key from password and salt
	key := DeriveKey(password, salt)

	// 4. Create AES cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// 5. Create GCM cipher mode
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM mode: %w", err)
	}

	// 6. Decrypt the data
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("failed to decrypt data (incorrect password or corrupted data)")
	}

	return plaintext, nil
}

// IsEncrypted detects if the provided data is an encrypted envelope.
func IsEncrypted(data []byte) bool {
	var envelope struct {
		Encrypted bool `json:"encrypted"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false
	}
	return envelope.Encrypted
}
