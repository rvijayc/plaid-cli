package cmd

import (
	"testing"

	"plaid-cli/pkg/config"
)

func TestResolveUpdateTarget(t *testing.T) {
	cfg := &config.Config{
		Items: []config.LinkedItem{
			{ItemID: "item_a"},
			{ItemID: "item_b"},
			{ItemID: "item_c"},
		},
	}

	t.Run("by 1-based list number", func(t *testing.T) {
		idx, err := resolveUpdateTarget(cfg, []string{"2"})
		if err != nil || idx != 1 {
			t.Errorf("got idx=%d err=%v, want idx=1 nil", idx, err)
		}
	})

	t.Run("by item_id", func(t *testing.T) {
		idx, err := resolveUpdateTarget(cfg, []string{"item_c"})
		if err != nil || idx != 2 {
			t.Errorf("got idx=%d err=%v, want idx=2 nil", idx, err)
		}
	})

	t.Run("out-of-range number falls through to not-found", func(t *testing.T) {
		if _, err := resolveUpdateTarget(cfg, []string{"99"}); err == nil {
			t.Error("expected error for out-of-range number, got nil")
		}
	})

	t.Run("unknown arg errors", func(t *testing.T) {
		if _, err := resolveUpdateTarget(cfg, []string{"nope"}); err == nil {
			t.Error("expected error for unknown arg, got nil")
		}
	})
}

func TestFindItemByAccountID_CachedDirectory(t *testing.T) {
	cfg := &config.Config{
		Items: []config.LinkedItem{
			{ItemID: "item_a", Accounts: []config.Account{{AccountID: "acc_1"}, {AccountID: "acc_2"}}},
			{ItemID: "item_b", Accounts: []config.Account{{AccountID: "acc_3"}}},
		},
	}

	// Account present in the cached directory resolves without any API call, so a
	// nil Plaid client is safe here and proves no live fetch is attempted.
	t.Run("found in first item", func(t *testing.T) {
		if idx, err := findItemByAccountID(cfg, nil, "acc_2"); err != nil || idx != 0 {
			t.Errorf("got idx=%d err=%v, want idx=0 nil", idx, err)
		}
	})
	t.Run("found in second item", func(t *testing.T) {
		if idx, err := findItemByAccountID(cfg, nil, "acc_3"); err != nil || idx != 1 {
			t.Errorf("got idx=%d err=%v, want idx=1 nil", idx, err)
		}
	})
}
