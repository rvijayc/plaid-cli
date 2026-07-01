package cmd

import (
	"math"
	"testing"

	"plaid-cli/pkg/config"

	"github.com/plaid/plaid-go/v20/plaid"
)

func testSecurity() plaid.Security {
	return plaid.Security{
		SecurityId:   "sec_xyz",
		Name:         *plaid.NewNullableString(strPtr("Vanguard Total Stock Market ETF")),
		TickerSymbol: *plaid.NewNullableString(strPtr("VTI")),
		Type:         *plaid.NewNullableString(strPtr("etf")),
	}
}

func TestHoldingRowFrom(t *testing.T) {
	h := plaid.Holding{
		AccountId:        "acc_brokerage",
		SecurityId:       "sec_xyz",
		InstitutionPrice: 258.13,
		InstitutionValue: 3226.63,
		CostBasis:        *plaid.NewNullableFloat64(f64Ptr(2900.00)),
		Quantity:         12.5,
	}
	secMap := securityMap([]plaid.Security{testSecurity()})
	dir := map[string]config.Account{"acc_brokerage": {AccountID: "acc_brokerage", Name: "Brokerage"}}

	row := holdingRowFrom(h, secMap, dir)

	if row.Security != "Vanguard Total Stock Market ETF" || row.Ticker != "VTI" || row.Type != "etf" {
		t.Errorf("security join wrong: name=%q ticker=%q type=%q", row.Security, row.Ticker, row.Type)
	}
	if row.GainLoss == nil || math.Abs(*row.GainLoss-326.63) > 0.01 {
		t.Errorf("GainLoss = %v, want ~326.63", row.GainLoss)
	}
	if row.GainLossPct == nil || math.Abs(*row.GainLossPct-11.26) > 0.05 {
		t.Errorf("GainLossPct = %v, want ~11.26", row.GainLossPct)
	}
}

func TestHoldingRowFrom_NoCostBasis(t *testing.T) {
	h := plaid.Holding{
		AccountId:        "acc_brokerage",
		SecurityId:       "sec_xyz",
		InstitutionPrice: 100,
		InstitutionValue: 1000,
		CostBasis:        *plaid.NewNullableFloat64(nil),
		Quantity:         10,
	}
	row := holdingRowFrom(h, securityMap([]plaid.Security{testSecurity()}), map[string]config.Account{})
	if row.CostBasis != nil || row.GainLoss != nil || row.GainLossPct != nil {
		t.Errorf("expected nil cost basis / gain-loss when cost basis absent, got cb=%v gl=%v pct=%v",
			row.CostBasis, row.GainLoss, row.GainLossPct)
	}
}

func TestSecurityLabel(t *testing.T) {
	secMap := securityMap([]plaid.Security{testSecurity()})

	t.Run("known", func(t *testing.T) {
		name, ticker, typ := securityLabel(secMap, "sec_xyz")
		if name != "Vanguard Total Stock Market ETF" || ticker != "VTI" || typ != "etf" {
			t.Errorf("got name=%q ticker=%q type=%q", name, ticker, typ)
		}
	})
	t.Run("unknown id falls back to short id", func(t *testing.T) {
		name, ticker, _ := securityLabel(secMap, "sec_unknown_longvalue")
		if ticker != "" {
			t.Errorf("ticker = %q, want empty for unknown security", ticker)
		}
		if name != config.ShortAccountID("sec_unknown_longvalue") {
			t.Errorf("name = %q, want short id fallback", name)
		}
	})
	t.Run("empty id", func(t *testing.T) {
		name, ticker, typ := securityLabel(secMap, "")
		if name != "" || ticker != "" || typ != "" {
			t.Errorf("expected all empty for empty security id, got %q/%q/%q", name, ticker, typ)
		}
	})
}

func TestInvestmentTxnRowFrom(t *testing.T) {
	tx := plaid.InvestmentTransaction{
		InvestmentTransactionId: "itx_1",
		AccountId:               "acc_brokerage",
		SecurityId:              *plaid.NewNullableString(strPtr("sec_xyz")),
		Date:                    "2026-05-20",
		Name:                    "BUY VTI",
		Quantity:                1.0,
		Price:                   257.40,
		Amount:                  257.40,
		Fees:                    *plaid.NewNullableFloat64(f64Ptr(0.00)),
		Type:                    plaid.InvestmentTransactionType("buy"),
		Subtype:                 plaid.InvestmentTransactionSubtype("buy"),
	}
	secMap := securityMap([]plaid.Security{testSecurity()})
	row := investmentTxnRowFrom(tx, secMap, map[string]config.Account{})

	if row.Type != "buy" || row.Subtype != "buy" {
		t.Errorf("type/subtype = %q/%q, want buy/buy", row.Type, row.Subtype)
	}
	if row.Ticker != "VTI" {
		t.Errorf("ticker = %q, want VTI", row.Ticker)
	}

	// Cash transaction: no security id -> blank ticker, no panic.
	cash := plaid.InvestmentTransaction{
		InvestmentTransactionId: "itx_2",
		AccountId:               "acc_brokerage",
		SecurityId:              *plaid.NewNullableString(nil),
		Date:                    "2026-05-21",
		Name:                    "DIVIDEND",
		Type:                    plaid.InvestmentTransactionType("cash"),
		Subtype:                 plaid.InvestmentTransactionSubtype("dividend"),
	}
	crow := investmentTxnRowFrom(cash, secMap, map[string]config.Account{})
	if crow.Ticker != "" {
		t.Errorf("cash txn ticker = %q, want empty", crow.Ticker)
	}
}

func TestGainLossStr(t *testing.T) {
	if got := gainLossStr(f64Ptr(326.63), f64Ptr(11.26)); got != "+$326.63 (+11.3%)" {
		t.Errorf("positive gain = %q, want +$326.63 (+11.3%%)", got)
	}
	if got := gainLossStr(f64Ptr(-50.0), f64Ptr(-2.1)); got != "-$50.00 (-2.1%)" {
		t.Errorf("loss = %q, want -$50.00 (-2.1%%)", got)
	}
	if got := gainLossStr(nil, nil); got != "-" {
		t.Errorf("nil gain = %q, want -", got)
	}
	if got := gainLossStr(f64Ptr(10.0), nil); got != "+$10.00" {
		t.Errorf("gain without pct = %q, want +$10.00", got)
	}
}

func TestTrimFloat(t *testing.T) {
	cases := map[float64]string{
		12.5:   "12.5",
		10:     "10",
		1.2345: "1.2345",
		0:      "0",
	}
	for in, want := range cases {
		if got := trimFloat(in); got != want {
			t.Errorf("trimFloat(%v) = %q, want %q", in, got, want)
		}
	}
}
