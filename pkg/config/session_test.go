package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionSaveLoad(t *testing.T) {
	// 1. Setup temporary home directory
	tempDir, err := os.MkdirTemp("", "plaid-cli-session-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")

	os.Setenv("HOME", tempDir)
	os.Setenv("USERPROFILE", tempDir)

	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	}()

	password := "session-test-pass"

	// 2. Save Session
	err = SaveSession(password)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// 3. Load Session
	loadedPassword, err := LoadSession()
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if loadedPassword != password {
		t.Errorf("expected password to match, got %s, want %s", loadedPassword, password)
	}

	// 4. Verify file permissions are 0600 on unix (we can just verify file existence on Windows/Unix)
	sessionPath, err := getSessionPath()
	if err != nil {
		t.Fatalf("failed to get session path: %v", err)
	}

	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("session file does not exist: %v", err)
	}

	// On Unix, check permission is 0600 (owner read-write only)
	// On Windows, Mode().Perm() may look different (e.g. 0666 or 0600), but we check we can read/write it.
	if os.PathSeparator == '/' { // Unix-like
		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Errorf("expected session file permission to be 0600, got %O", perm)
		}
	}
}

func TestSessionExpiration(t *testing.T) {
	// 1. Setup temporary home directory
	tempDir, err := os.MkdirTemp("", "plaid-cli-session-expire-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")

	os.Setenv("HOME", tempDir)
	os.Setenv("USERPROFILE", tempDir)

	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	}()

	password := "expire-test-pass"

	// Save Session
	err = SaveSession(password)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// To simulate expiration, we read the session.json, decrypt it, re-save it with an expired time.
	sessionPath, err := getSessionPath()
	if err != nil {
		t.Fatalf("failed to get session path: %v", err)
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("failed to read session file: %v", err)
	}

	key, err := getMachineKey()
	if err != nil {
		t.Fatalf("failed to get machine key: %v", err)
	}

	decrypted, err := decryptSession(data, key)
	if err != nil {
		t.Fatalf("failed to decrypt session data: %v", err)
	}

	var session SessionData
	if err := json.Unmarshal(decrypted, &session); err != nil {
		t.Fatalf("failed to unmarshal session: %v", err)
	}

	// Set expiration in the past
	session.ExpiresAt = time.Now().Add(-10 * time.Minute)

	jsonData, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("failed to marshal session: %v", err)
	}

	encrypted, err := encryptSession(jsonData, key)
	if err != nil {
		t.Fatalf("failed to encrypt session: %v", err)
	}

	err = os.WriteFile(sessionPath, encrypted, 0600)
	if err != nil {
		t.Fatalf("failed to overwrite session file: %v", err)
	}

	// Now try to load the session - it should return ErrSessionExpired and the file should be deleted
	_, err = LoadSession()
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}

	// Check if session file has been deleted
	_, err = os.Stat(sessionPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected session file to be deleted, but os.Stat returned: %v", err)
	}
}

func TestSessionInvalidDecryption(t *testing.T) {
	// 1. Setup temporary home directory
	tempDir, err := os.MkdirTemp("", "plaid-cli-session-invalid-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")

	os.Setenv("HOME", tempDir)
	os.Setenv("USERPROFILE", tempDir)

	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	}()

	sessionPath, err := getSessionPath()
	if err != nil {
		t.Fatalf("failed to get session path: %v", err)
	}

	// Write garbage to session file
	err = os.MkdirAll(filepath.Dir(sessionPath), 0700)
	if err != nil {
		t.Fatalf("failed to create session directory: %v", err)
	}
	err = os.WriteFile(sessionPath, []byte("garbage data that cannot be decrypted"), 0600)
	if err != nil {
		t.Fatalf("failed to write garbage session file: %v", err)
	}

	// Attempting to load should fail and the file should be deleted
	_, err = LoadSession()
	if err == nil {
		t.Error("expected load to fail for corrupted session file")
	}

	// Check if session file has been deleted
	_, err = os.Stat(sessionPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected garbage session file to be deleted, but os.Stat returned: %v", err)
	}
}

func TestClearSession(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plaid-cli-session-clear-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")

	os.Setenv("HOME", tempDir)
	os.Setenv("USERPROFILE", tempDir)

	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	}()

	// Clear session when file doesn't exist should succeed
	err = ClearSession()
	if err != nil {
		t.Errorf("ClearSession on non-existent file should succeed, got: %v", err)
	}

	// Save session
	err = SaveSession("pass")
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	sessionPath, err := getSessionPath()
	if err != nil {
		t.Fatalf("failed to get session path: %v", err)
	}

	// Clear session
	err = ClearSession()
	if err != nil {
		t.Errorf("failed to clear session: %v", err)
	}

	// Check if session file has been deleted
	_, err = os.Stat(sessionPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected session file to be deleted, but os.Stat returned: %v", err)
	}
}
