package cmd

import (
	"bufio"
	"fmt"
	"os"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"strings"
	"text/tabwriter"

	"github.com/plaid/plaid-go/v20/plaid"
	"github.com/spf13/cobra"
)

var removeForce bool

func init() {
	accountsRemoveCmd.Flags().BoolVar(&removeForce, "force", false, "Skip confirmation prompt")
	accountsCmd.AddCommand(accountsRemoveCmd)
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

var accountsRemoveCmd = &cobra.Command{
	Use:   "remove [item_id|account_id|number]",
	Short: "Remove a linked bank account",
	Long:  `Invalidate the access token with Plaid and remove the account from local config and cache.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}
		if len(cfg.Items) == 0 {
			return fmt.Errorf("no accounts linked")
		}

		plaidClient, err := client.NewPlaidClient(cfg)
		if err != nil {
			return err
		}

		// Backfill missing institution names so the list and confirmation are readable.
		namesUpdated := false
		for i := range cfg.Items {
			if cfg.Items[i].InstitutionName == "" {
				instID, instName, ferr := client.GetInstitutionInfo(plaidClient, cfg.Items[i].AccessToken)
				if ferr == nil && instName != "" {
					cfg.Items[i].InstitutionID = instID
					cfg.Items[i].InstitutionName = instName
					namesUpdated = true
				}
			}
		}
		if namesUpdated {
			_ = cfg.SaveConfig()
		}

		displayName := func(item config.LinkedItem) string {
			if item.InstitutionName != "" {
				return item.InstitutionName
			}
			return item.ItemID
		}

		// Resolve target item
		var target *config.LinkedItem

		if len(args) == 1 {
			arg := args[0]

			// 1. Match by 1-based list index
			var idx int
			if _, serr := fmt.Sscanf(arg, "%d", &idx); serr == nil && idx >= 1 && idx <= len(cfg.Items) {
				target = &cfg.Items[idx-1]
			}

			// 2. Match by item_id
			if target == nil {
				for i := range cfg.Items {
					if cfg.Items[i].ItemID == arg {
						target = &cfg.Items[i]
						break
					}
				}
			}

			// 3. Match by account_id — walk each item's accounts
			if target == nil {
				for i := range cfg.Items {
					accs, ferr := client.FetchAccounts(plaidClient, cfg.Items[i].AccessToken)
					if ferr != nil {
						continue
					}
					for _, acc := range accs {
						if acc.AccountId == arg {
							target = &cfg.Items[i]
							break
						}
					}
					if target != nil {
						break
					}
				}
			}

			if target == nil {
				return fmt.Errorf("no linked item found for %q", arg)
			}
		} else {
			// Interactive: print numbered list and prompt
			fmt.Println("Linked accounts:")
			for i, item := range cfg.Items {
				fmt.Printf("  [%d] %s\n", i+1, displayName(item))
			}
			fmt.Print("\nEnter number to remove: ")
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			var idx int
			if _, serr := fmt.Sscanf(line, "%d", &idx); serr != nil || idx < 1 || idx > len(cfg.Items) {
				return fmt.Errorf("invalid selection")
			}
			target = &cfg.Items[idx-1]
		}

		name := displayName(*target)

		// Confirm unless --force
		if !removeForce {
			fmt.Printf("Remove %q? This will invalidate the access token and delete all cached transactions. [y/N] ", name)
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		targetItemID := target.ItemID

		// Collect account IDs for cache purge before the token is invalidated
		accountIDs := map[string]struct{}{}
		accs, err := client.FetchAccounts(plaidClient, target.AccessToken)
		if err != nil {
			fmt.Printf("Warning: could not fetch account IDs for cache purge: %v\n", err)
		} else {
			for _, acc := range accs {
				accountIDs[acc.AccountId] = struct{}{}
			}
		}

		// Invalidate server-side
		if err := client.RemoveItem(plaidClient, target.AccessToken); err != nil {
			return fmt.Errorf("failed to remove item from Plaid: %w", err)
		}

		// Remove from config
		updated := cfg.Items[:0]
		for _, item := range cfg.Items {
			if item.ItemID != targetItemID {
				updated = append(updated, item)
			}
		}
		cfg.Items = updated
		if cfg.ItemID == targetItemID {
			cfg.ItemID = ""
			cfg.AccessToken = ""
		}
		if err := cfg.SaveConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		// Purge cache
		cache, err := config.LoadCache()
		if err != nil {
			return fmt.Errorf("failed to load cache for purge: %w", err)
		}
		delete(cache.Cursors, targetItemID)
		if len(accountIDs) > 0 {
			kept := cache.Transactions[:0]
			for _, tx := range cache.Transactions {
				if _, isTarget := accountIDs[tx.AccountId]; !isTarget {
					kept = append(kept, tx)
				}
			}
			purged := len(cache.Transactions) - len(kept)
			cache.Transactions = kept
			fmt.Printf("Purged %d cached transactions.\n", purged)
		}
		if err := cache.SaveCache(); err != nil {
			return fmt.Errorf("failed to save cache: %w", err)
		}

		fmt.Printf("Removed: %s\n", name)
		return nil
	},
}
