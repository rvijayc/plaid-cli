package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// SessionFile is the filename for storing the active session key.
const SessionFile = "session.json"

// SessionExpiryDuration is the time period before a session expires.
const SessionExpiryDuration = 15 * time.Minute

// ErrSessionExpired is returned when the loaded session is expired.
var ErrSessionExpired = errors.New("session expired")

// SessionData represents the structure of the session cache on disk.
type SessionData struct {
	Key       string    `json:"key"`        // The active password/key
	ExpiresAt time.Time `json:"expires_at"` // The expiration timestamp
}

// getMachineKey derives a machine-specific AES key from the OS hostname and the user's home directory.
func getMachineKey() ([]byte, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Combine hostname and homeDir into a SHA-256 hash to get a 256-bit key.
	hasher := sha256.New()
	hasher.Write([]byte(hostname))
	hasher.Write([]byte(homeDir))
	return hasher.Sum(nil), nil
}

// encryptSession encrypts bytes using AES-GCM and the machine key.
func encryptSession(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := aesGCM.Seal(nil, nonce, plaintext, nil)
	// Combine nonce + ciphertext
	return append(nonce, ciphertext...), nil
}

// decryptSession decrypts bytes using AES-GCM and the machine key.
func decryptSession(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("invalid session data length")
	}
	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

// getSessionPath returns the absolute path to the session.json file.
func getSessionPath() (string, error) {
	dir, err := GetDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SessionFile), nil
}

// SaveSession encrypts and saves the active password to ~/.plaid-cli/session.json.
// The file is written with strict user-only read/write permissions (0600).
func SaveSession(password string) error {
	if password == "" {
		return nil
	}
	sessionPath, err := getSessionPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(sessionPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	data := SessionData{
		Key:       password,
		ExpiresAt: time.Now().Add(SessionExpiryDuration),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	key, err := getMachineKey()
	if err != nil {
		return fmt.Errorf("failed to get machine key: %w", err)
	}

	encrypted, err := encryptSession(jsonData, key)
	if err != nil {
		return fmt.Errorf("failed to encrypt session: %w", err)
	}

	if err := os.WriteFile(sessionPath, encrypted, 0600); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
}

// LoadSession decrypts the session.json file and returns the active password if it is valid and not expired.
// If it is expired or invalid, it deletes the session file and returns an error.
func LoadSession() (string, error) {
	sessionPath, err := getSessionPath()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return "", err
	}

	key, err := getMachineKey()
	if err != nil {
		return "", fmt.Errorf("failed to get machine key: %w", err)
	}

	decrypted, err := decryptSession(data, key)
	if err != nil {
		// If decryption fails, clean up the corrupted file.
		_ = os.Remove(sessionPath)
		return "", fmt.Errorf("failed to decrypt session: %w", err)
	}

	var session SessionData
	if err := json.Unmarshal(decrypted, &session); err != nil {
		_ = os.Remove(sessionPath)
		return "", fmt.Errorf("failed to unmarshal session: %w", err)
	}

	if time.Now().After(session.ExpiresAt) {
		_ = os.Remove(sessionPath)
		return "", ErrSessionExpired
	}

	return session.Key, nil
}

// ClearSession deletes the session.json file from disk.
func ClearSession() error {
	sessionPath, err := getSessionPath()
	if err != nil {
		return err
	}
	err = os.Remove(sessionPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
