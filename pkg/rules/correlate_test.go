package rules

import (
	"plaid-cli/pkg/config"
	"testing"

	"github.com/plaid/plaid-go/v20/plaid"
)

func dtx(id, name string, amount float64, account, date string) plaid.Transaction {
	return plaid.Transaction{
		TransactionId: id,
		Name:          name,
		Amount:        amount,
		AccountId:     account,
		Date:          date,
	}
}

func TestCorrelateTransfers(t *testing.T) {
	names := map[string]string{"card": "Robinhood CC", "chk": "Checking"}

	tests := []struct {
		name      string
		txs       []plaid.Transaction
		opts      CorrelateOptions
		wantPairs map[string]string // debitTxID -> creditAccountName
	}{
		{
			name: "matches debit to opposite credit within window",
			txs: []plaid.Transaction{
				dtx("d1", "Online Transfer / Payment: Debit", 2620.34, "chk", "2026-06-16"),
				dtx("c1", "Payment", -2620.34, "card", "2026-06-15"),
			},
			opts:      CorrelateOptions{NameContains: DefaultPaymentName, MaxDays: 3},
			wantPairs: map[string]string{"d1": "Robinhood CC"},
		},
		{
			name: "no counter-credit on a linked account leaves transfer untouched",
			txs: []plaid.Transaction{
				dtx("d1", "Online Transfer / Payment: Debit", 414.05, "chk", "2026-06-02"),
			},
			opts:      CorrelateOptions{NameContains: DefaultPaymentName, MaxDays: 3},
			wantPairs: map[string]string{},
		},
		{
			name: "outside the settlement window does not match",
			txs: []plaid.Transaction{
				dtx("d1", "Online Transfer / Payment: Debit", 100.00, "chk", "2026-06-20"),
				dtx("c1", "Payment", -100.00, "card", "2026-06-10"),
			},
			opts:      CorrelateOptions{NameContains: DefaultPaymentName, MaxDays: 3},
			wantPairs: map[string]string{},
		},
		{
			name: "credit on the same account is not a payment",
			txs: []plaid.Transaction{
				dtx("d1", "Online Transfer / Payment: Debit", 50.00, "chk", "2026-06-05"),
				dtx("c1", "Refund", -50.00, "chk", "2026-06-05"),
			},
			opts:      CorrelateOptions{NameContains: DefaultPaymentName, MaxDays: 3},
			wantPairs: map[string]string{},
		},
		{
			name: "each credit is consumed by at most one debit",
			txs: []plaid.Transaction{
				dtx("d1", "Online Transfer / Payment: Debit", 80.21, "chk", "2026-06-01"),
				dtx("d2", "Online Transfer / Payment: Debit", 80.21, "chk", "2026-06-02"),
				dtx("c1", "Payment", -80.21, "card", "2026-06-01"),
			},
			opts:      CorrelateOptions{NameContains: DefaultPaymentName, MaxDays: 3},
			wantPairs: map[string]string{"d1": "Robinhood CC"},
		},
		{
			name: "name filter excludes non-payment debits",
			txs: []plaid.Transaction{
				dtx("d1", "Home Depot", 226.66, "chk", "2026-06-12"),
				dtx("c1", "Payment", -226.66, "card", "2026-06-12"),
			},
			opts:      CorrelateOptions{NameContains: DefaultPaymentName, MaxDays: 3},
			wantPairs: map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CorrelateTransfers(tc.txs, names, tc.opts)
			if len(got) != len(tc.wantPairs) {
				t.Fatalf("got %d matches, want %d: %+v", len(got), len(tc.wantPairs), got)
			}
			for _, m := range got {
				want, ok := tc.wantPairs[m.DebitTxID]
				if !ok {
					t.Errorf("unexpected match for debit %s", m.DebitTxID)
					continue
				}
				if m.CreditAccount != want {
					t.Errorf("debit %s: got credit account %q, want %q", m.DebitTxID, m.CreditAccount, want)
				}
			}
		})
	}
}

func TestPaymentOverride(t *testing.T) {
	m := PaymentMatch{DebitTxID: "d1", CreditAccount: "Amex Gold", Amount: 380.53}

	ignored := PaymentOverride(m, true)
	if ignored.Source != config.SourceCorrelate {
		t.Errorf("expected source %q, got %q", config.SourceCorrelate, ignored.Source)
	}
	if ignored.DisplayName != "Payment: Amex Gold" {
		t.Errorf("unexpected display name %q", ignored.DisplayName)
	}
	if !ignored.Ignored {
		t.Error("expected ignored override when ignore=true")
	}

	visible := PaymentOverride(m, false)
	if visible.Ignored {
		t.Error("expected non-ignored override when ignore=false")
	}
}
