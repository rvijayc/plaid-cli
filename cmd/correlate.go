package cmd

import (
	"fmt"
	"os"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"plaid-cli/pkg/rules"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	correlateDryRunFlag     bool
	correlateDaysFlag       int
	correlateNameFlag       string
	correlateSourceAcctFlag string
	correlateSourceTypes    []string
	correlateDestTypes      []string
	correlateNoIgnoreFlag   bool
	correlateOfflineFlag    bool
)

func init() {
	correlateCmd.Flags().BoolVar(&correlateDryRunFlag, "dry-run", false, "Print matches without writing overrides")
	correlateCmd.Flags().IntVar(&correlateDaysFlag, "days", 3, "Settlement window in days between a payment debit and its counter-credit")
	correlateCmd.Flags().StringVar(&correlateNameFlag, "name", "", "Optional substring the outbound debit name must contain (empty matches any)")
	correlateCmd.Flags().StringVar(&correlateSourceAcctFlag, "source-account", "", "Restrict the debit side to a single account ID")
	correlateCmd.Flags().StringSliceVar(&correlateSourceTypes, "source-type", rules.DefaultSourceTypes, "Plaid account types allowed on the debit side (comma-separated; empty for any)")
	correlateCmd.Flags().StringSliceVar(&correlateDestTypes, "dest-type", rules.DefaultDestTypes, "Plaid account types allowed on the credit/destination side (comma-separated; empty for any)")
	correlateCmd.Flags().BoolVar(&correlateNoIgnoreFlag, "no-ignore", false, "Keep matched payments visible in spend summaries (default hides them)")
	correlateCmd.Flags().BoolVar(&correlateOfflineFlag, "offline", false, "Skip the Plaid account lookup; disables type filters and labels by account ID")

	rulesCmd.AddCommand(correlateCmd)
}

var correlateCmd = &cobra.Command{
	Use:   "correlate",
	Short: "Classify inter-account payments (credit-card and loan payments) by matching transfers across accounts",
	Long: `Match an outbound payment debit to the equal-and-opposite credit it settled on
another linked account, and label each debit with the account it paid.

Payments are identified by account type, not transaction name, so the feature is
bank-agnostic: by default a debit on a depository account (cash leaving
checking/savings) is paired with a credit on a credit-card or loan account — a
card payment or an installment paydown (mortgage, student, auto). Adjust the
sides with --source-type / --dest-type.

Unlike a rule, this correlates two transactions across accounts by amount and
date, which the rules engine cannot express. A transfer whose money does not
land on another linked account of a destination type (an external payee, or a
savings sweep when only credit/loan are destinations) has no qualifying
counter-credit and is left untouched. Matched payments are hidden from spend
summaries by default to avoid double-counting purchases already recorded on the
destination account.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cache, err := config.LoadCache()
		if err != nil {
			return err
		}

		// Build the account directory (name + Plaid type) used for type-based
		// matching and readable labels. Offline mode cannot know account types,
		// so it disables the type filters and labels by account ID.
		accts := map[string]rules.AccountInfo{}
		sourceTypes := correlateSourceTypes
		destTypes := correlateDestTypes
		if correlateOfflineFlag {
			fmt.Fprintln(os.Stderr, "Offline: account types unavailable; disabling type filters (matching any inter-account transfer).")
			sourceTypes = nil
			destTypes = nil
		} else {
			cfg, cerr := config.LoadConfig()
			if cerr != nil {
				return cerr
			}
			plaidClient, perr := client.NewPlaidClient(cfg)
			if perr != nil {
				return perr
			}
			activeIdx, skippedItems := cfg.ActiveItemIndexes()
			config.WarnSkippedItems(skippedItems, cfg.Environment)
			for _, idx := range activeIdx {
				item := cfg.Items[idx]
				accs, ferr := client.FetchAccounts(plaidClient, item.AccessToken)
				if ferr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not fetch accounts for item %s: %v\n", item.ItemID, ferr)
					continue
				}
				for _, a := range accs {
					accts[a.AccountId] = rules.AccountInfo{Name: a.Name, Type: string(a.Type)}
				}
			}
		}

		matches := rules.CorrelateTransfers(cache.Transactions, accts, rules.CorrelateOptions{
			NameContains:    correlateNameFlag,
			MaxDays:         correlateDaysFlag,
			SourceAccountID: correlateSourceAcctFlag,
			SourceTypes:     sourceTypes,
			DestTypes:       destTypes,
		})

		if len(matches) == 0 {
			fmt.Fprintln(os.Stderr, "No inter-account payments matched.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "DATE\tAMOUNT\tKIND\tPAID")
		fmt.Fprintln(w, "----\t------\t----\t----")
		for _, m := range matches {
			kind := m.CreditType
			if kind == "" {
				kind = "-"
			}
			fmt.Fprintf(w, "%s\t%.2f\t%s\t%s\n", m.DebitDate, m.Amount, kind, m.CreditAccount)
		}
		_ = w.Flush()

		if correlateDryRunFlag {
			fmt.Fprintf(os.Stderr, "Dry run: %d payments would be classified (no changes written).\n", len(matches))
			return nil
		}

		if cache.Overrides == nil {
			cache.Overrides = make(map[string]config.Override)
		}

		// Drop prior correlation overrides so re-running reflects the current cache.
		for id, o := range cache.Overrides {
			if o.Source == config.SourceCorrelate {
				delete(cache.Overrides, id)
			}
		}

		for _, m := range matches {
			cache.Overrides[m.DebitTxID] = rules.PaymentOverride(m, !correlateNoIgnoreFlag)
		}

		if err := cache.SaveCache(); err != nil {
			return fmt.Errorf("failed to save cache: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Classified %d inter-account payments.\n", len(matches))
		return nil
	},
}
