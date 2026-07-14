package rules

import (
	"fmt"
	"math"
	"plaid-cli/pkg/config"
	"sort"
	"strings"
	"time"

	"github.com/plaid/plaid-go/v43/plaid"
)

// DefaultPaymentName is one bank's wording for an outbound online transfer. It
// is offered only as a convenience value for the optional --name filter;
// correlation no longer depends on it, identifying payments by account type
// instead so the feature is bank-agnostic.
const DefaultPaymentName = "Online Transfer / Payment: Debit"

// dateLayout is the format Plaid uses for transaction dates.
const dateLayout = "2006-01-02"

// Plaid account types (account.type) used for correlation classification.
const (
	TypeDepository = "depository"
	TypeCredit     = "credit"
	TypeLoan       = "loan"
)

// DefaultSourceTypes and DefaultDestTypes describe the canonical money flow for
// a bill payment: cash leaves a depository account (checking/savings) and lands
// on a credit card or an installment/loan account (mortgage, student, auto).
var (
	DefaultSourceTypes = []string{TypeDepository}
	DefaultDestTypes   = []string{TypeCredit, TypeLoan}
)

// AccountInfo is the per-account metadata correlation needs: a display name and
// the Plaid account type used to classify the source and destination sides.
type AccountInfo struct {
	Name string
	Type string
}

// CorrelateOptions configures cross-account payment correlation.
type CorrelateOptions struct {
	// NameContains optionally restricts the debit (outbound) side to transactions
	// whose name contains this substring. Empty means match any name.
	NameContains string
	// MaxDays is the inclusive settlement window: a debit and its counter-credit
	// may post up to this many days apart in either direction.
	MaxDays int
	// SourceAccountID, when set, restricts the debit side to a single account.
	SourceAccountID string
	// SourceTypes restricts the debit side to accounts of these Plaid types
	// (e.g. "depository"). Empty means any type.
	SourceTypes []string
	// DestTypes restricts the credit (destination) side to accounts of these
	// Plaid types (e.g. "credit", "loan"). Empty means any type.
	DestTypes []string
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
	CreditType     string // Plaid type of the destination account
	Amount         float64
}

// indexedTx pairs a transaction's slice index with its parsed date.
type indexedTx struct {
	idx  int
	when time.Time
}

// CorrelateTransfers matches outbound payment debits to the inbound credits on
// other linked accounts that settled them, by equal magnitude within MaxDays.
//
// Payments are identified by account type rather than transaction name, so the
// feature is bank-agnostic: by default a debit on a depository account (cash
// leaving checking/savings) is paired with a credit on a credit-card or loan
// account (a card payment or an installment paydown). Matching is greedy and
// one-to-one — each credit is consumed by at most one debit, debits processed
// oldest-first.
//
// Correlation is self-limiting: a transfer whose money does not land on another
// linked account of a destination type (an external payee, a savings sweep when
// only credit/loan are destinations) has no qualifying counter-credit and is
// left untouched.
//
// accts maps account IDs to name/type metadata; a missing entry falls back to
// the raw account ID for the label and an empty type (which fails any non-empty
// type filter).
func CorrelateTransfers(txs []plaid.Transaction, accts map[string]AccountInfo, opts CorrelateOptions) []PaymentMatch {
	if opts.MaxDays < 0 {
		opts.MaxDays = 0
	}
	sourceOK := typeFilter(opts.SourceTypes)
	destOK := typeFilter(opts.DestTypes)

	// Bucket candidate credits (amount < 0) on a qualifying destination account
	// by their rounded absolute amount so each debit only scans plausible
	// counterparties.
	creditsByAmount := make(map[int64][]indexedTx)
	for i, tx := range txs {
		if tx.Amount >= 0 {
			continue
		}
		if !destOK(accts[tx.AccountId].Type) {
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

	// Collect and order qualifying debits oldest-first for deterministic matching.
	var debits []indexedTx
	for i, tx := range txs {
		if tx.Amount <= 0 {
			continue
		}
		if !sourceOK(accts[tx.AccountId].Type) {
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
		info := accts[credit.AccountId]
		name := info.Name
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
			CreditType:     info.Type,
			Amount:         debit.Amount,
		})
	}

	return matches
}

// PaymentOverride builds the display override written for a matched debit. The
// label and category are derived from the destination account type so a card
// payment, a loan paydown, and an account transfer each read correctly. When
// ignore is true the payment is hidden from spend summaries to avoid
// double-counting purchases already recorded on the destination account.
func PaymentOverride(m PaymentMatch, ignore bool) config.Override {
	display, category, tags := paymentLabel(m.CreditType, m.CreditAccount)
	return config.Override{
		DisplayName: display,
		Category:    category,
		Tags:        tags,
		Ignored:     ignore,
		Source:      config.SourceCorrelate,
	}
}

// paymentLabel renders the display name, category, and tags for a payment based
// on the destination account type.
func paymentLabel(destType, name string) (display, category string, tags []string) {
	switch destType {
	case TypeCredit:
		return "Payment: " + name, "Transfer: Credit Card Payment", []string{"transfer", "card-payment"}
	case TypeLoan:
		return "Payment: " + name, "Transfer: Loan Payment", []string{"transfer", "loan-payment"}
	default:
		return "Transfer: " + name, "Transfer: Account Transfer", []string{"transfer"}
	}
}

// typeFilter returns a predicate reporting whether an account type is allowed.
// An empty allow-list permits any type (including unknown/empty).
func typeFilter(allowed []string) func(string) bool {
	if len(allowed) == 0 {
		return func(string) bool { return true }
	}
	set := make(map[string]bool, len(allowed))
	for _, t := range allowed {
		set[strings.ToLower(strings.TrimSpace(t))] = true
	}
	return func(t string) bool { return set[strings.ToLower(t)] }
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
