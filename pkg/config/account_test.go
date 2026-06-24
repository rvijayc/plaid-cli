package config

import "testing"

func TestShortAccountID(t *testing.T) {
	if got := ShortAccountID("PrwPL8gxZEUz0y3qEMmZ"); got != "PrwPL8gx" {
		t.Errorf("got %q, want %q", got, "PrwPL8gx")
	}
	if got := ShortAccountID("abc"); got != "abc" {
		t.Errorf("short IDs should pass through unchanged, got %q", got)
	}
}

func TestAccountLabel(t *testing.T) {
	cfg := &Config{
		Items: []LinkedItem{
			{
				ItemID: "item_1",
				Accounts: []Account{
					{AccountID: "acc_gold_1234567890", Name: "Amex Gold", Mask: "5528", Subtype: "credit card"},
					{AccountID: "acc_noname_000000000", Name: ""},
				},
			},
		},
	}

	tests := []struct {
		name      string
		accountID string
		want      string
	}{
		{"known account renders name with short id", "acc_gold_1234567890", "Amex Gold (acc_gold)"},
		{"missing name falls back to short id", "acc_noname_000000000", "acc_nona"},
		{"unknown account falls back to short id", "totallyUnknownId", "totallyU"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfg.AccountLabel(tc.accountID); got != tc.want {
				t.Errorf("AccountLabel(%q) = %q, want %q", tc.accountID, got, tc.want)
			}
		})
	}
}

func TestAccountDirectory(t *testing.T) {
	cfg := &Config{
		Items: []LinkedItem{
			{ItemID: "i1", Accounts: []Account{{AccountID: "a1", Name: "Checking"}}},
			{ItemID: "i2", Accounts: []Account{{AccountID: "a2", Name: "Savings"}}},
		},
	}
	dir := cfg.AccountDirectory()
	if len(dir) != 2 {
		t.Fatalf("expected 2 accounts in directory, got %d", len(dir))
	}
	if dir["a2"].Name != "Savings" {
		t.Errorf("expected a2 -> Savings, got %q", dir["a2"].Name)
	}
}
