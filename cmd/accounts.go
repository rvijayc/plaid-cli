package cmd

import (
	"fmt"
	"os"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"text/tabwriter"

	"github.com/plaid/plaid-go/v20/plaid"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(accountsCmd)
}

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "List bank accounts and their balances",
	Long:  `Fetch and display account details, types, and real-time balances associated with your authenticated Plaid Item.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Load configuration
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		if len(cfg.Items) == 0 {
			return fmt.Errorf("no accounts linked. Please run 'plaid-cli login' first")
		}

		// 2. Initialize Plaid API Client
		plaidClient, err := client.NewPlaidClient(cfg)
		if err != nil {
			return err
		}

		// 3. Fetch Accounts for all linked items
		fmt.Println("Fetching accounts and balances...")
		var allAccounts []plaid.AccountBase
		for idx, item := range cfg.Items {
			institutionStr := item.ItemID
			if len(institutionStr) > 8 {
				institutionStr = institutionStr[:8]
			}
			fmt.Printf("[%d/%d] Fetching accounts for Item %s...\n", idx+1, len(cfg.Items), institutionStr)
			accounts, err := client.FetchAccounts(plaidClient, item.AccessToken)
			if err != nil {
				fmt.Printf("Warning: failed to fetch accounts for Item %s: %v\n", item.ItemID, err)
				continue
			}
			allAccounts = append(allAccounts, accounts...)
		}

		if len(allAccounts) == 0 {
			fmt.Println("No accounts found.")
			return nil
		}

		// 4. Print Accounts Table
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "ACCOUNT ID\tNAME\tTYPE (SUBTYPE)\tCURR BAL\tAVAIL BAL\tCURRENCY")
		fmt.Fprintln(w, "----------\t----\t--------------\t--------\t---------\t--------")

		for _, acc := range allAccounts {
			// Extract account details safely
			id := acc.AccountId
			name := acc.Name
			accType := string(acc.Type)
			subtype := ""
			if acc.Subtype.IsSet() && acc.Subtype.Get() != nil {
				subtype = string(*acc.Subtype.Get())
			}

			balances := acc.Balances
			currBal := 0.0
			if balances.Current.IsSet() && balances.Current.Get() != nil {
				currBal = *balances.Current.Get()
			}

			availBalStr := "-"
			if balances.Available.IsSet() && balances.Available.Get() != nil {
				availBalStr = fmt.Sprintf("%.2f", *balances.Available.Get())
			}

			currency := "USD"
			if balances.IsoCurrencyCode.IsSet() && balances.IsoCurrencyCode.Get() != nil {
				currency = *balances.IsoCurrencyCode.Get()
			}

			typeStr := accType
			if subtype != "" {
				typeStr = fmt.Sprintf("%s (%s)", accType, subtype)
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%.2f\t%s\t%s\n",
				id,
				name,
				typeStr,
				currBal,
				availBalStr,
				currency,
			)
		}
		_ = w.Flush()
		fmt.Println()

		return nil
	},
}
