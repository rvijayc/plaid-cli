package cmd

import (
	"plaid-cli/pkg/config"
	"testing"

	"github.com/plaid/plaid-go/v20/plaid"
)

func strPtr(s string) *string   { return &s }
func f64Ptr(f float64) *float64 { return &f }
func boolPtr(b bool) *bool      { return &b }

func TestCreditRowFrom(t *testing.T) {
	c := plaid.CreditCardLiability{
		AccountId: *plaid.NewNullableString(strPtr("acc_card")),
		Aprs: []plaid.APR{
			{AprType: "balance_transfer_apr", AprPercentage: 24.99},
			{AprType: "purchase_apr", AprPercentage: 19.99},
		},
		LastStatementBalance: *plaid.NewNullableFloat64(f64Ptr(1240.55)),
		MinimumPaymentAmount: *plaid.NewNullableFloat64(f64Ptr(35.00)),
		NextPaymentDueDate:   *plaid.NewNullableString(strPtr("2026-05-28")),
		IsOverdue:            *plaid.NewNullableBool(boolPtr(false)),
	}

	dir := map[string]config.Account{"acc_card": {AccountID: "acc_card", Name: "Sapphire"}}
	row := creditRowFrom(c, dir)

	if row.AccountID != "acc_card" {
		t.Errorf("AccountID = %q, want acc_card", row.AccountID)
	}
	if row.PurchaseAPR == nil || *row.PurchaseAPR != 19.99 {
		t.Errorf("PurchaseAPR = %v, want 19.99 (must pick purchase_apr, not the first APR)", row.PurchaseAPR)
	}
	if row.LastStatementBalance == nil || *row.LastStatementBalance != 1240.55 {
		t.Errorf("LastStatementBalance = %v, want 1240.55", row.LastStatementBalance)
	}
	if row.IsOverdue == nil || *row.IsOverdue {
		t.Errorf("IsOverdue = %v, want false", row.IsOverdue)
	}
}

func TestCreditRowFrom_MissingFields(t *testing.T) {
	// All nullable fields unset, no APRs — every pointer should be nil, no panic.
	c := plaid.CreditCardLiability{
		AccountId: *plaid.NewNullableString(strPtr("acc_card")),
	}
	row := creditRowFrom(c, map[string]config.Account{})
	if row.PurchaseAPR != nil {
		t.Errorf("PurchaseAPR = %v, want nil when no APRs present", row.PurchaseAPR)
	}
	if row.LastStatementBalance != nil || row.MinimumPayment != nil || row.IsOverdue != nil {
		t.Errorf("expected nil pointers for unset nullable fields")
	}
}

func TestMortgageRowFrom(t *testing.T) {
	m := plaid.MortgageLiability{
		AccountId: "acc_mortgage",
		InterestRate: plaid.MortgageInterestRate{
			Percentage: *plaid.NewNullableFloat64(f64Ptr(3.25)),
			Type:       *plaid.NewNullableString(strPtr("fixed")),
		},
		NextMonthlyPayment:         *plaid.NewNullableFloat64(f64Ptr(1830.00)),
		OriginationPrincipalAmount: *plaid.NewNullableFloat64(f64Ptr(410000.00)),
		MaturityDate:               *plaid.NewNullableString(strPtr("2051-04-15")),
	}
	row := mortgageRowFrom(m, map[string]config.Account{})
	if row.InterestRate == nil || *row.InterestRate != 3.25 {
		t.Errorf("InterestRate = %v, want 3.25", row.InterestRate)
	}
	if row.InterestRateType != "fixed" {
		t.Errorf("InterestRateType = %q, want fixed", row.InterestRateType)
	}
	if row.MaturityDate != "2051-04-15" {
		t.Errorf("MaturityDate = %q, want 2051-04-15", row.MaturityDate)
	}
}

func TestFilterLiabilities(t *testing.T) {
	base := liabilitiesOutput{
		Credit:   []creditRow{{AccountID: "a"}, {AccountID: "b"}},
		Student:  []studentRow{{AccountID: "a"}},
		Mortgage: []mortgageRow{{AccountID: "c"}},
	}

	t.Run("type filter keeps only one class", func(t *testing.T) {
		out := filterLiabilities(base, "", "credit")
		if len(out.Credit) != 2 || len(out.Student) != 0 || len(out.Mortgage) != 0 {
			t.Errorf("type=credit gave credit=%d student=%d mortgage=%d, want 2/0/0",
				len(out.Credit), len(out.Student), len(out.Mortgage))
		}
	})

	t.Run("account-id filter narrows across classes", func(t *testing.T) {
		// Rebuild base; filterLiabilities mutates via slice[:0] reuse.
		b := liabilitiesOutput{
			Credit:   []creditRow{{AccountID: "a"}, {AccountID: "b"}},
			Student:  []studentRow{{AccountID: "a"}},
			Mortgage: []mortgageRow{{AccountID: "c"}},
		}
		out := filterLiabilities(b, "a", "")
		if len(out.Credit) != 1 || out.Credit[0].AccountID != "a" {
			t.Errorf("credit after account filter = %+v, want single 'a'", out.Credit)
		}
		if len(out.Student) != 1 {
			t.Errorf("student after account filter = %d, want 1", len(out.Student))
		}
		if len(out.Mortgage) != 0 {
			t.Errorf("mortgage after account filter = %d, want 0", len(out.Mortgage))
		}
	})
}

func TestFormatHelpers(t *testing.T) {
	if got := money(nil); got != "-" {
		t.Errorf("money(nil) = %q, want -", got)
	}
	if got := money(f64Ptr(1234.5)); got != "$1234.50" {
		t.Errorf("money(1234.5) = %q, want $1234.50", got)
	}
	if got := moneyRaw(nil); got != "" {
		t.Errorf("moneyRaw(nil) = %q, want empty", got)
	}
	if got := percent(f64Ptr(19.99)); got != "19.99%" {
		t.Errorf("percent(19.99) = %q, want 19.99%%", got)
	}
	if got := percent(nil); got != "-" {
		t.Errorf("percent(nil) = %q, want -", got)
	}
	if got := boolStr(boolPtr(true)); got != "yes" {
		t.Errorf("boolStr(true) = %q, want yes", got)
	}
	if got := boolStr(nil); got != "-" {
		t.Errorf("boolStr(nil) = %q, want -", got)
	}
	if got := dash(""); got != "-" {
		t.Errorf("dash(\"\") = %q, want -", got)
	}
}
