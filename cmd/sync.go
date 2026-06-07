package cmd

import (
	"fmt"
	"os"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"plaid-cli/pkg/rules"

	"github.com/plaid/plaid-go/v20/plaid"
	"github.com/spf13/cobra"
)

var (
	resetFlag     bool
	itemIdFlag    string
	accountIdFlag string
)

func init() {
	syncCmd.Flags().BoolVar(&resetFlag, "reset", false, "Reset the sync cursor and perform a full historical sync from scratch")
	syncCmd.Flags().StringVar(&itemIdFlag, "item-id", "", "Sync transactions only for a specific Plaid Item ID")
	syncCmd.Flags().StringVar(&accountIdFlag, "account-id", "", "Sync transactions only for the Item associated with a specific Plaid Account ID")
	rootCmd.AddCommand(syncCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync transaction data from Plaid locally",
	Long: `Incrementally fetch transaction changes (added, modified, and removed) from Plaid
using cursor-based synchronization and save them to your local cache (~/.plaid-cli/cache.json).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Load config
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		if len(cfg.Items) == 0 {
			return fmt.Errorf("no accounts linked. Please run 'plaid-cli login' first")
		}

		// 2. Load cache
		cache, err := config.LoadCache()
		if err != nil {
			return err
		}

		// 3. Initialize Plaid Client
		plaidClient, err := client.NewPlaidClient(cfg)
		if err != nil {
			return err
		}

		// 4. Resolve target items to sync
		if itemIdFlag != "" && accountIdFlag != "" {
			return fmt.Errorf("cannot specify both --item-id and --account-id")
		}

		var targetItems []config.LinkedItem
		if itemIdFlag != "" {
			found := false
			for _, item := range cfg.Items {
				if item.ItemID == itemIdFlag {
					targetItems = append(targetItems, item)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("item ID %s not found in linked accounts config", itemIdFlag)
			}
		} else if accountIdFlag != "" {
			fmt.Fprintln(os.Stderr, "Resolving account ID to find its parent Plaid Item...")
			var matchedItem *config.LinkedItem
			for _, item := range cfg.Items {
				accounts, err := client.FetchAccounts(plaidClient, item.AccessToken)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to fetch accounts for Item %s: %v\n", item.ItemID, err)
					continue
				}
				for _, acc := range accounts {
					if acc.AccountId == accountIdFlag {
						matchedItem = &item
						break
					}
				}
				if matchedItem != nil {
					break
				}
			}
			if matchedItem == nil {
				return fmt.Errorf("account ID %s not found in any linked items", accountIdFlag)
			}
			targetItems = append(targetItems, *matchedItem)
		} else {
			targetItems = cfg.Items
		}

		// Map existing transactions by ID
		txMap := make(map[string]plaid.Transaction)
		for _, tx := range cache.Transactions {
			txMap[tx.TransactionId] = tx
		}

		if resetFlag {
			if len(targetItems) == len(cfg.Items) {
				cache.Cursors = make(map[string]string)
				cache.Cursor = ""
				txMap = make(map[string]plaid.Transaction)
				fmt.Println("Reset flag enabled. Clearing all local cursors and cached transactions from scratch...")
			} else {
				fmt.Println("Reset flag enabled for targeted sync. Resetting cursor and cached transactions for target items...")
			}
		}

		fmt.Println("Starting transaction synchronization...")

		addedCount := 0
		modifiedCount := 0
		removedCount := 0
		changedIDs := make(map[string]bool)

		for idx, item := range targetItems {
			institutionStr := item.ItemID
			if len(institutionStr) > 8 {
				institutionStr = institutionStr[:8]
			}
			fmt.Printf("[%d/%d] Syncing transactions for Item %s...\n", idx+1, len(targetItems), institutionStr)

			// 1. Fetch and check linked accounts for the current item
			accounts, err := client.FetchAccounts(plaidClient, item.AccessToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to check accounts for Item %s: %v. Skipping this item.\n", item.ItemID, err)
				continue
			}

			fmt.Println("  Checking linked accounts:")
			for _, acc := range accounts {
				subtypeStr := ""
				if acc.Subtype.IsSet() && acc.Subtype.Get() != nil {
					subtypeStr = string(*acc.Subtype.Get())
				}
				typeDisplay := string(acc.Type)
				if subtypeStr != "" {
					typeDisplay = fmt.Sprintf("%s (%s)", acc.Type, subtypeStr)
				}
				fmt.Printf("    - %s [ID: %s, Type: %s]\n", acc.Name, acc.AccountId, typeDisplay)
			}

			// 2. Reset handling (clear targeted cache) if requested
			if resetFlag && len(targetItems) < len(cfg.Items) {
				delete(cache.Cursors, item.ItemID)
				if item.ItemID == cfg.ItemID {
					cache.Cursor = ""
				}

				accMap := make(map[string]bool)
				for _, acc := range accounts {
					accMap[acc.AccountId] = true
				}

				// Remove existing cached transactions belonging to this item's accounts
				for txID, tx := range txMap {
					if accMap[tx.AccountId] {
						delete(txMap, txID)
					}
				}
			}

			itemCursor := cache.Cursors[item.ItemID]
			if itemCursor != "" {
				fmt.Printf("  Resuming sync from cursor: %s...\n", itemCursor[:15]+"...[truncated]")
			} else {
				fmt.Println("  Initial synchronization. This might take a moment...")
			}

			hasMore := true
			page := 1
			var syncErr error
			for hasMore {
				fmt.Printf("  Fetching transaction page %d...\n", page)
				nextCursor, added, modified, removed, more, err := client.SyncTransactionsPage(plaidClient, item.AccessToken, itemCursor)
				if err != nil {
					syncErr = fmt.Errorf("sync failed on page %d: %w", page, err)
					break
				}

				// Add
				for _, tx := range added {
					txMap[tx.TransactionId] = tx
					changedIDs[tx.TransactionId] = true
					addedCount++
				}

				// Modify
				for _, tx := range modified {
					txMap[tx.TransactionId] = tx
					changedIDs[tx.TransactionId] = true
					modifiedCount++
				}

				// Remove
				for _, rtx := range removed {
					if rtx.TransactionId != nil {
						delete(txMap, *rtx.TransactionId)
						removedCount++
					}
				}

				itemCursor = nextCursor
				hasMore = more
				page++
			}

			if syncErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to sync transactions for Item %s: %v. Continuing...\n", item.ItemID, syncErr)
				continue
			}

			cache.Cursors[item.ItemID] = itemCursor
		}

		// Convert map back to slice
		updatedTxList := make([]plaid.Transaction, 0, len(txMap))
		for _, tx := range txMap {
			updatedTxList = append(updatedTxList, tx)
		}

		// Save back to cache
		cache.Transactions = updatedTxList

		err = cache.SaveCache()
		if err != nil {
			return fmt.Errorf("failed to save transactions cache: %w", err)
		}

		// Apply categorization rules to the transactions added/modified in this sync.
		overrideCount := 0
		rf, rErr := rules.LoadRules()
		if rErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load rules, skipping auto-categorization: %v\n", rErr)
		} else if len(rf.Rules) > 0 && len(changedIDs) > 0 {
			ids := make([]string, 0, len(changedIDs))
			for id := range changedIDs {
				ids = append(ids, id)
			}
			overrideCount = rules.ApplyAll(cache, rf, ids)
			if err := cache.SaveCache(); err != nil {
				return fmt.Errorf("failed to save cache after applying rules: %w", err)
			}
		}

		fmt.Println("\nSynchronization completed successfully!")
		fmt.Printf("Added:      %d\n", addedCount)
		fmt.Printf("Modified:   %d\n", modifiedCount)
		fmt.Printf("Removed:    %d\n", removedCount)
		fmt.Printf("Overrides:  %d\n", overrideCount)
		fmt.Printf("Total Cache Size: %d transactions\n", len(updatedTxList))
		fmt.Println("You can run 'plaid-cli transactions' to view and filter them.")

		return nil
	},
}
