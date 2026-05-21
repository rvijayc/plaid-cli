package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadSaveEncrypted(t *testing.T) {
	// 1. Setup temporary home directory
	tempDir, err := os.MkdirTemp("", "plaid-cli-test-home-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Save original env variables
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")

	// Set temp environment
	os.Setenv("HOME", tempDir)
	os.Setenv("USERPROFILE", tempDir)

	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	}()

	// Define test configuration
	cfg := &Config{
		ClientID:    "test-client-id",
		Secret:      "test-secret",
		Environment: "sandbox",
		Secure:      true,
		Items: []LinkedItem{
			{ItemID: "item-1", AccessToken: "access-token-1"},
		},
	}

	// 2. Configure password
	password := "super-secure-pass"
	os.Setenv("PLAID_CLI_PASSWORD", password)
	defer os.Unsetenv("PLAID_CLI_PASSWORD")

	// Set password directly to clear any interactive prompt blockages
	SetPassword(password)

	// 3. Save Config (should encrypt)
	err = cfg.SaveConfig()
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// 4. Verify file exists and is encrypted on disk
	dir, err := GetDir()
	if err != nil {
		t.Fatalf("failed to get config dir: %v", err)
	}

	configPath := filepath.Join(dir, ConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file from disk: %v", err)
	}

	if !IsEncrypted(data) {
		t.Fatal("expected saved configuration file to be encrypted")
	}

	// 5. Load Config (should decrypt)
	// Clear password memory cache first to force env fallback
	SetPassword("")
	loadedCfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to load/decrypt config: %v", err)
	}

	if loadedCfg.ClientID != cfg.ClientID || loadedCfg.Secret != cfg.Secret {
		t.Errorf("decrypted fields do not match original: got ClientID=%s, Secret=%s", loadedCfg.ClientID, loadedCfg.Secret)
	}

	if len(loadedCfg.Items) != 1 || loadedCfg.Items[0].ItemID != "item-1" {
		t.Errorf("decrypted items list mismatch: got %+v", loadedCfg.Items)
	}
}

func TestCacheLoadSaveEncrypted(t *testing.T) {
	// 1. Setup temporary home directory
	tempDir, err := os.MkdirTemp("", "plaid-cli-test-home-cache-*")
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

	// 2. Set environment password and mock active config as secure
	password := "super-secure-pass"
	SetPassword(password)

	cfg := &Config{
		ClientID:    "test-client-id",
		Secret:      "test-secret",
		Environment: "sandbox",
		Secure:      true,
	}
	err = cfg.SaveConfig()
	if err != nil {
		t.Fatalf("failed to save secure config: %v", err)
	}

	// 3. Define cache data
	cache := &Cache{
		Cursors: map[string]string{
			"item-1": "cursor-xyz",
		},
	}

	// 4. Save Cache (should encrypt because config is secure)
	err = cache.SaveCache()
	if err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	// 5. Verify cache is encrypted on disk
	dir, err := GetDir()
	if err != nil {
		t.Fatalf("failed to get config dir: %v", err)
	}

	cachePath := filepath.Join(dir, CacheFile)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("failed to read cache file: %v", err)
	}

	if !IsEncrypted(data) {
		t.Fatal("expected cache file on disk to be encrypted")
	}

	// 6. Load Cache (should decrypt)
	loadedCache, err := LoadCache()
	if err != nil {
		t.Fatalf("failed to load/decrypt cache: %v", err)
	}

	if loadedCache.Cursors["item-1"] != "cursor-xyz" {
		t.Errorf("decrypted cursors mismatch: got %+v", loadedCache.Cursors)
	}
}
