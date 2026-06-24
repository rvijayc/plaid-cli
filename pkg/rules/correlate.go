package rules

import (
	"fmt"
	"math"
	"plaid-cli/pkg/config"
	"sort"
	"strings"
	"time"

	"github.com/plaid/plaid-go/v20/plaid"
)

// DefaultPaymentName is the transaction name banks use for outbound online
// transfers/payments. It is the default debit-side filter for correlation.
const DefaultPaymentName = "Online Transfer / Payment: Debit"

// dateLayout is the format Plaid uses for transaction dates.
const dateLayout = "2006-01-02"

// CorrelateOptions configures cross-account payment correlation.
type CorrelateOptions struct {
	// NameContains restricts the debit (outbound) side to transactions whose
	// name contains this substring. Empty means match any name.
	NameContains string
	// MaxDays is the inclusive settlement window: a debit and its counter-credit
	// may post up to this many days apart in either direction.
	MaxDays int
	// SourceAccountID, when set, restricts the debit side to a single account.
	SourceAccountID string
}

// PaymentMatch links an outbound debit on one account to the equal-and-opposite
// credit on another linked account that it settled.
type PaymentMatch struct {
	DebitTxID      string
	DebitAccountID string
	DebitDate      string
	CreditTxID     string
	CreditAccount  string // resolved display name (falls back to account ID)
	CreditAcctID   string
	Amount         float64
}

// txByDate sorts a slice of indices into a transaction slice by ascending date.
type indexedTx struct {
	idx  int
	when time.Time
}

// CorrelateTransfers matches outbound payment debits to the inbound credits on
// other linked accounts that settled them, by equal magnitude within MaxDays.
//
// Matching is greedy and one-to-one: each credit is consumed by at most one
// debit, and debits are processed oldest-first so deterministic pairs win. A
// transfer whose money does not land in another linked account (an external
// payee, e.g. an auto loan or utility autopay) has no counter-credit and is
// therefore never matched — correlation is self-limiting and will not touch
// genuine outbound bills.
//
// acctNames maps account IDs to human-readable names for labeling; a missing
// entry falls back to the raw account ID.
func CorrelateTransfers(txs []plaid.Transaction, acctNames map[string]string, opts CorrelateOptions) []PaymentMatch {
	if opts.MaxDays < 0 {
		opts.MaxDays = 0
	}

	// Bucket candidate credits (amount < 0) by their rounded absolute amount so
	// each debit only scans plausible counterparties.
	creditsByAmount := make(map[int64][]indexedTx)
	for i, tx := range txs {
		if tx.Amount >= 0 {
			continue
		}
		when, err := time.Parse(dateLayout, tx.Date)
		if err != nil {
			continue
		}
		key := cents(-tx.Amount)
		creditsByAmount[key] = append(creditsByAmount[key], indexedTx{idx: i, when: when})
	}
	for key := range creditsByAmount {
		c := creditsByAmount[key]
		sort.Slice(c, func(a, b int) bool { return c[a].when.Before(c[b].when) })
		creditsByAmount[key] = c
	}

	// Collect and order debits oldest-first for deterministic greedy matching.
	var debits []indexedTx
	for i, tx := range txs {
		if tx.Amount <= 0 {
			continue
		}
		if opts.SourceAccountID != "" && tx.AccountId != opts.SourceAccountID {
			continue
		}
		if opts.NameContains != "" && !containsFold(tx.Name, opts.NameContains) {
			continue
		}
		when, err := time.Parse(dateLayout, tx.Date)
		if err != nil {
			continue
		}
		debits = append(debits, indexedTx{idx: i, when: when})
	}
	sort.Slice(debits, func(a, b int) bool { return debits[a].when.Before(debits[b].when) })

	consumed := make(map[int]bool)
	var matches []PaymentMatch

	for _, d := range debits {
		debit := txs[d.idx]
		key := cents(debit.Amount)
		candidates := creditsByAmount[key]

		best := -1
		var bestDelta int
		for _, c := range candidates {
			if consumed[c.idx] {
				continue
			}
			credit := txs[c.idx]
			// A payment must move money between two different accounts.
			if credit.AccountId == debit.AccountId {
				continue
			}
			delta := absDays(c.when, d.when)
			if delta > opts.MaxDays {
				continue
			}
			if best == -1 || delta < bestDelta {
				best = c.idx
				bestDelta = delta
			}
		}

		if best == -1 {
			continue
		}
		consumed[best] = true
		credit := txs[best]
		name := acctNames[credit.AccountId]
		if name == "" {
			name = credit.AccountId
		}
		matches = append(matches, PaymentMatch{
			DebitTxID:      debit.TransactionId,
			DebitAccountID: debit.AccountId,
			DebitDate:      debit.Date,
			CreditTxID:     credit.TransactionId,
			CreditAccount:  name,
			CreditAcctID:   credit.AccountId,
			Amount:         debit.Amount,
		})
	}

	return matches
}

// PaymentOverride builds the display override written for a matched debit.
// When ignore is true the payment is hidden from spend summaries to avoid
// double-counting the purchases already recorded on the destination account.
func PaymentOverride(m PaymentMatch, ignore bool) config.Override {
	return config.Override{
		DisplayName: "Payment: " + m.CreditAccount,
		Category:    "Transfer: Credit Card Payment",
		Tags:        []string{"transfer", "card-payment"},
		Ignored:     ignore,
		Source:      config.SourceCorrelate,
	}
}

// cents converts a dollar amount to integer cents for exact comparison,
// avoiding float equality pitfalls.
func cents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// absDays returns the absolute whole-day difference between two dates.
func absDays(a, b time.Time) int {
	d := int(a.Sub(b).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

// containsFold reports whether s contains substr, case-insensitively.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// String is a helper for command output; kept here so the command layer can
// render a one-line description of a match without re-deriving fields.
func (m PaymentMatch) String() string {
	return fmt.Sprintf("%s  %.2f  -> %s", m.DebitDate, m.Amount, m.CreditAccount)
}
