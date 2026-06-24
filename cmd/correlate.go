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
	correlateNoIgnoreFlag   bool
	correlateOfflineFlag    bool
)

func init() {
	correlateCmd.Flags().BoolVar(&correlateDryRunFlag, "dry-run", false, "Print matches without writing overrides")
	correlateCmd.Flags().IntVar(&correlateDaysFlag, "days", 3, "Settlement window in days between a payment debit and its counter-credit")
	correlateCmd.Flags().StringVar(&correlateNameFlag, "name", rules.DefaultPaymentName, "Substring the outbound debit name must contain")
	correlateCmd.Flags().StringVar(&correlateSourceAcctFlag, "source-account", "", "Restrict the debit side to a single account ID")
	correlateCmd.Flags().BoolVar(&correlateNoIgnoreFlag, "no-ignore", false, "Keep matched payments visible in spend summaries (default hides them)")
	correlateCmd.Flags().BoolVar(&correlateOfflineFlag, "offline", false, "Skip the Plaid account-name lookup; label by account ID")

	rulesCmd.AddCommand(correlateCmd)
}

var correlateCmd = &cobra.Command{
	Use:   "correlate",
	Short: "Classify inter-account payments (e.g. credit-card payments) by matching transfers across accounts",
	Long: `Match outbound payment debits (such as "Online Transfer / Payment: Debit") to the
equal-and-opposite credit they settled on another linked account, and label each
debit with the account it paid.

Unlike a rule, this correlates two transactions across accounts by amount and
date — something the rules engine cannot express. A transfer whose money does not
land in another linked account (an external auto loan or utility autopay) has no
counter-credit and is left untouched, so only genuine inter-account payments are
classified. Matched payments are hidden from spend summaries by default to avoid
double-counting purchases already recorded on the destination account.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cache, err := config.LoadCache()
		if err != nil {
			return err
		}

		// Best-effort account-name map for readable labels.
		acctNames := map[string]string{}
		if !correlateOfflineFlag {
			cfg, cerr := config.LoadConfig()
			if cerr != nil {
				return cerr
			}
			plaidClient, perr := client.NewPlaidClient(cfg)
			if perr != nil {
				return perr
			}
			for _, item := range cfg.Items {
				accs, ferr := client.FetchAccounts(plaidClient, item.AccessToken)
				if ferr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not fetch account names for item %s: %v\n", item.ItemID, ferr)
					continue
				}
				for _, a := range accs {
					acctNames[a.AccountId] = a.Name
				}
			}
		}

		matches := rules.CorrelateTransfers(cache.Transactions, acctNames, rules.CorrelateOptions{
			NameContains:    correlateNameFlag,
			MaxDays:         correlateDaysFlag,
			SourceAccountID: correlateSourceAcctFlag,
		})

		if len(matches) == 0 {
			fmt.Fprintln(os.Stderr, "No inter-account payments matched.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "DATE\tAMOUNT\tPAID")
		fmt.Fprintln(w, "----\t------\t----")
		for _, m := range matches {
			fmt.Fprintf(w, "%s\t%.2f\t%s\n", m.DebitDate, m.Amount, m.CreditAccount)
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
