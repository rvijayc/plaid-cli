package config

import (
	"os"
	"testing"
)

func TestActiveItemIndexes(t *testing.T) {
	cfg := &Config{
		Environment: "sandbox",
		Items: []LinkedItem{
			{ItemID: "i1", Environment: "sandbox"},
			{ItemID: "i2", Environment: "production"},
			{ItemID: "i3", Environment: "sandbox"},
			{ItemID: "i4"}, // unset: treated as active regardless of cfg.Environment
		},
	}

	active, skipped := cfg.ActiveItemIndexes()

	wantActive := []int{0, 2, 3}
	if len(active) != len(wantActive) {
		t.Fatalf("active = %v, want %v", active, wantActive)
	}
	for i, idx := range wantActive {
		if active[i] != idx {
			t.Errorf("active[%d] = %d, want %d", i, active[i], idx)
		}
	}

	if len(skipped) != 1 || skipped[0].ItemID != "i2" {
		t.Errorf("skipped = %v, want [i2]", skipped)
	}
}

func TestActiveItemIndexesAllMatch(t *testing.T) {
	cfg := &Config{
		Environment: "production",
		Items: []LinkedItem{
			{ItemID: "i1", Environment: "production"},
			{ItemID: "i2", Environment: "production"},
		},
	}

	active, skipped := cfg.ActiveItemIndexes()

	if len(skipped) != 0 {
		t.Errorf("expected no skipped items, got %v", skipped)
	}
	if len(active) != 2 {
		t.Errorf("expected both items active, got %v", active)
	}
}

func TestLoadConfigMigratesMissingItemEnvironment(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plaid-cli-test-home-env-*")
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

	cfg := &Config{
		ClientID:    "cid",
		Secret:      "sec",
		Environment: "sandbox",
		Items: []LinkedItem{
			{ItemID: "legacy_item", AccessToken: "tok"}, // no Environment set, as if linked pre-migration
		},
	}
	if err := cfg.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig() error: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if len(loaded.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(loaded.Items))
	}
	if loaded.Items[0].Environment != "sandbox" {
		t.Errorf("expected legacy item backfilled with Environment %q, got %q", "sandbox", loaded.Items[0].Environment)
	}
}
