package rules

import (
	"plaid-cli/pkg/config"
	"testing"

	"github.com/plaid/plaid-go/v43/plaid"
)

func tx(id, name string, amount float64, account string, category []string) plaid.Transaction {
	return plaid.Transaction{
		TransactionId: id,
		Name:          name,
		Amount:        amount,
		AccountId:     account,
		Category:      category,
	}
}

func TestMatchNameContains(t *testing.T) {
	r := Rule{Conditions: Conditions{NameContains: "venmo"}}
	if !r.Match(tx("1", "VENMO PAYMENT", 50, "acc", nil)) {
		t.Error("expected case-insensitive substring match")
	}
	if r.Match(tx("2", "Starbucks", 5, "acc", nil)) {
		t.Error("expected no match for non-matching name")
	}
}

func TestMatchNameRegex(t *testing.T) {
	r := Rule{Conditions: Conditions{NameRegex: `^UBER\s+\*`}}
	if !r.Match(tx("1", "UBER *TRIP", 12, "acc", nil)) {
		t.Error("expected regex match")
	}
	if r.Match(tx("2", "LYFT RIDE", 12, "acc", nil)) {
		t.Error("expected no regex match")
	}

	// Invalid regex should never match.
	bad := Rule{Conditions: Conditions{NameRegex: `[`}}
	if bad.Match(tx("3", "anything", 1, "acc", nil)) {
		t.Error("invalid regex should not match")
	}
}

func TestMatchAmountBounds(t *testing.T) {
	r := Rule{Conditions: Conditions{AmountMin: 50, AmountMax: 200}}
	if !r.Match(tx("1", "x", 100, "acc", nil)) {
		t.Error("100 should be within [50,200]")
	}
	if r.Match(tx("2", "x", 40, "acc", nil)) {
		t.Error("40 below min should not match")
	}
	if r.Match(tx("3", "x", 250, "acc", nil)) {
		t.Error("250 above max should not match")
	}
	// Inclusive bounds.
	if !r.Match(tx("4", "x", 50, "acc", nil)) || !r.Match(tx("5", "x", 200, "acc", nil)) {
		t.Error("bounds should be inclusive")
	}
}

func TestMatchCombinedConditions(t *testing.T) {
	r := Rule{Conditions: Conditions{
		NameContains: "venmo",
		AmountMin:    50,
		AmountMax:    200,
		AccountID:    "acc1",
	}}
	if !r.Match(tx("1", "VENMO ELECTRIC", 120, "acc1", nil)) {
		t.Error("all conditions met should match")
	}
	// Wrong account fails the AND.
	if r.Match(tx("2", "VENMO ELECTRIC", 120, "acc2", nil)) {
		t.Error("wrong account should fail combined match")
	}
	// Amount out of range fails.
	if r.Match(tx("3", "VENMO ELECTRIC", 10, "acc1", nil)) {
		t.Error("low amount should fail combined match")
	}
}

func TestMatchCategoryIs(t *testing.T) {
	r := Rule{Conditions: Conditions{CategoryIs: "groceries"}}
	if !r.Match(tx("1", "Target", 30, "acc", []string{"Shops", "Supermarkets and Groceries"})) {
		t.Error("expected category substring match")
	}
	if r.Match(tx("2", "Target", 30, "acc", []string{"Travel"})) {
		t.Error("expected no category match")
	}
}

func TestApply(t *testing.T) {
	r := Rule{
		ID: "rule_x",
		Actions: Actions{
			Rename:      "Electric Bill",
			SetCategory: "Bills & Utilities > Electric",
			Tags:        []string{"reimbursement"},
			Ignore:      true,
		},
	}
	o := r.Apply(tx("1", "VENMO", 100, "acc", nil))
	if o.DisplayName != "Electric Bill" {
		t.Errorf("rename not applied: %q", o.DisplayName)
	}
	if o.Category != "Bills & Utilities > Electric" {
		t.Errorf("category not applied: %q", o.Category)
	}
	if len(o.Tags) != 1 || o.Tags[0] != "reimbursement" {
		t.Errorf("tags not applied: %v", o.Tags)
	}
	if !o.Ignored {
		t.Error("ignore not applied")
	}
	if o.RuleID != "rule_x" {
		t.Errorf("rule id not stamped: %q", o.RuleID)
	}
	if o.Manual {
		t.Error("rule-generated override should not be manual")
	}
}

func TestApplyAll(t *testing.T) {
	cache := &config.Cache{
		Transactions: []plaid.Transaction{
			tx("t1", "VENMO PAYMENT", 100, "acc", nil),
			tx("t2", "STARBUCKS", 5, "acc", nil),
			tx("t3", "MANUAL TX", 1, "acc", nil),
		},
		Overrides: map[string]config.Override{
			"t3": {DisplayName: "Hand Edited", Manual: true},
		},
	}
	rf := &RulesFileData{Rules: []Rule{
		{ID: "r1", Enabled: true, Conditions: Conditions{NameContains: "venmo"}, Actions: Actions{Rename: "Venmo Renamed"}},
		{ID: "r2", Enabled: false, Conditions: Conditions{NameContains: "starbucks"}, Actions: Actions{Rename: "Coffee"}},
	}}

	count := ApplyAll(cache, rf, nil)
	if count != 1 {
		t.Errorf("expected 1 override written, got %d", count)
	}
	if cache.Overrides["t1"].DisplayName != "Venmo Renamed" {
		t.Errorf("t1 not overridden: %+v", cache.Overrides["t1"])
	}
	if _, ok := cache.Overrides["t2"]; ok {
		t.Error("disabled rule should not produce an override")
	}
	if cache.Overrides["t3"].DisplayName != "Hand Edited" {
		t.Error("manual override must be preserved")
	}
}

func TestMergeOverrides(t *testing.T) {
	txs := []plaid.Transaction{
		tx("t1", "RAW NAME", 10, "acc", []string{"Shops"}),
		tx("t2", "OTHER", 20, "acc", nil),
	}
	overrides := map[string]config.Override{
		"t1": {DisplayName: "Nice Name", Category: "Custom > Cat", Tags: []string{"a", "b"}, Ignored: true},
	}
	display := MergeOverrides(txs, overrides)
	if len(display) != 2 {
		t.Fatalf("expected 2 display rows, got %d", len(display))
	}

	if display[0].DisplayName != "Nice Name" {
		t.Errorf("override name not applied: %q", display[0].DisplayName)
	}
	if display[0].DisplayCategory != "Custom > Cat" {
		t.Errorf("override category not applied: %q", display[0].DisplayCategory)
	}
	if display[0].Tags != "a,b" {
		t.Errorf("tags not joined: %q", display[0].Tags)
	}
	if display[0].Ignored != "true" {
		t.Errorf("ignored not set: %q", display[0].Ignored)
	}

	// No override falls back to raw values.
	if display[1].DisplayName != "OTHER" {
		t.Errorf("raw name expected, got %q", display[1].DisplayName)
	}
	if display[1].Tags != "" || display[1].Ignored != "" {
		t.Error("unset override fields should be empty")
	}
}
