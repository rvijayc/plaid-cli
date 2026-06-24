package rules

import (
	"plaid-cli/pkg/config"
	"reflect"
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

// defaultOpts mirrors the command defaults: depository debits -> credit/loan credits.
func defaultOpts() CorrelateOptions {
	return CorrelateOptions{
		MaxDays:     3,
		SourceTypes: DefaultSourceTypes,
		DestTypes:   DefaultDestTypes,
	}
}

func TestCorrelateTransfers(t *testing.T) {
	accts := map[string]AccountInfo{
		"chk":  {Name: "Checking", Type: TypeDepository},
		"sav":  {Name: "Savings", Type: TypeDepository},
		"card": {Name: "Robinhood CC", Type: TypeCredit},
		"loan": {Name: "Mortgage", Type: TypeLoan},
	}

	tests := []struct {
		name      string
		txs       []plaid.Transaction
		opts      CorrelateOptions
		wantPairs map[string]string // debitTxID -> creditAccountName
	}{
		{
			name: "depository debit matches credit-card payment within window",
			txs: []plaid.Transaction{
				dtx("d1", "Online Transfer / Payment: Debit", 2620.34, "chk", "2026-06-16"),
				dtx("c1", "Payment", -2620.34, "card", "2026-06-15"),
			},
			opts:      defaultOpts(),
			wantPairs: map[string]string{"d1": "Robinhood CC"},
		},
		{
			name: "depository debit matches a loan paydown",
			txs: []plaid.Transaction{
				dtx("d1", "ACH Debit", 1691.22, "chk", "2026-06-08"),
				dtx("c1", "Principal Payment", -1691.22, "loan", "2026-06-08"),
			},
			opts:      defaultOpts(),
			wantPairs: map[string]string{"d1": "Mortgage"},
		},
		{
			name: "savings sweep is excluded when only credit/loan are destinations",
			txs: []plaid.Transaction{
				dtx("d1", "Transfer", 400.00, "chk", "2026-06-10"),
				dtx("c1", "Transfer", -400.00, "sav", "2026-06-10"),
			},
			opts:      defaultOpts(),
			wantPairs: map[string]string{},
		},
		{
			name: "broadening dest-type captures the savings transfer",
			txs: []plaid.Transaction{
				dtx("d1", "Transfer", 400.00, "chk", "2026-06-10"),
				dtx("c1", "Transfer", -400.00, "sav", "2026-06-10"),
			},
			opts:      CorrelateOptions{MaxDays: 3, SourceTypes: DefaultSourceTypes, DestTypes: nil},
			wantPairs: map[string]string{"d1": "Savings"},
		},
		{
			name: "credit-side debit is not a source (card spend ignored)",
			txs: []plaid.Transaction{
				// A purchase on the card is a positive amount but the card is not a
				// depository source, so it must never be treated as a payment debit.
				dtx("d1", "Best Buy", 500.00, "card", "2026-06-12"),
				dtx("c1", "Refund", -500.00, "chk", "2026-06-12"),
			},
			opts:      defaultOpts(),
			wantPairs: map[string]string{},
		},
		{
			name: "outside the settlement window does not match",
			txs: []plaid.Transaction{
				dtx("d1", "Payment", 100.00, "chk", "2026-06-20"),
				dtx("c1", "Payment", -100.00, "card", "2026-06-10"),
			},
			opts:      defaultOpts(),
			wantPairs: map[string]string{},
		},
		{
			name: "credit on the same account is not a payment",
			txs: []plaid.Transaction{
				dtx("d1", "Adjustment", 50.00, "chk", "2026-06-05"),
				dtx("c1", "Refund", -50.00, "chk", "2026-06-05"),
			},
			opts:      defaultOpts(),
			wantPairs: map[string]string{},
		},
		{
			name: "each credit is consumed by at most one debit",
			txs: []plaid.Transaction{
				dtx("d1", "Payment", 80.21, "chk", "2026-06-01"),
				dtx("d2", "Payment", 80.21, "chk", "2026-06-02"),
				dtx("c1", "Payment", -80.21, "card", "2026-06-01"),
			},
			opts:      defaultOpts(),
			wantPairs: map[string]string{"d1": "Robinhood CC"},
		},
		{
			name: "name filter narrows the debit side",
			txs: []plaid.Transaction{
				dtx("d1", "Home Depot", 226.66, "chk", "2026-06-12"),
				dtx("c1", "Payment", -226.66, "card", "2026-06-12"),
			},
			opts: CorrelateOptions{
				NameContains: "online transfer",
				MaxDays:      3,
				SourceTypes:  DefaultSourceTypes,
				DestTypes:    DefaultDestTypes,
			},
			wantPairs: map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CorrelateTransfers(tc.txs, accts, tc.opts)
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

func TestPaymentOverrideByDestType(t *testing.T) {
	tests := []struct {
		name         string
		destType     string
		wantDisplay  string
		wantCategory string
		wantTags     []string
	}{
		{"credit card", TypeCredit, "Payment: Amex Gold", "Transfer: Credit Card Payment", []string{"transfer", "card-payment"}},
		{"loan", TypeLoan, "Payment: Amex Gold", "Transfer: Loan Payment", []string{"transfer", "loan-payment"}},
		{"depository", TypeDepository, "Transfer: Amex Gold", "Transfer: Account Transfer", []string{"transfer"}},
		{"unknown", "", "Transfer: Amex Gold", "Transfer: Account Transfer", []string{"transfer"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := PaymentMatch{CreditAccount: "Amex Gold", CreditType: tc.destType, Amount: 380.53}
			o := PaymentOverride(m, true)
			if o.Source != config.SourceCorrelate {
				t.Errorf("expected source %q, got %q", config.SourceCorrelate, o.Source)
			}
			if !o.Ignored {
				t.Error("expected ignored override when ignore=true")
			}
			if o.DisplayName != tc.wantDisplay {
				t.Errorf("display: got %q, want %q", o.DisplayName, tc.wantDisplay)
			}
			if o.Category != tc.wantCategory {
				t.Errorf("category: got %q, want %q", o.Category, tc.wantCategory)
			}
			if !reflect.DeepEqual(o.Tags, tc.wantTags) {
				t.Errorf("tags: got %v, want %v", o.Tags, tc.wantTags)
			}
		})
	}

	if PaymentOverride(PaymentMatch{CreditType: TypeCredit}, false).Ignored {
		t.Error("expected non-ignored override when ignore=false")
	}
}
