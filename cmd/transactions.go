package cmd

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"plaid-cli/pkg/config"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/plaid/plaid-go/v20/plaid"
	"github.com/spf13/cobra"
)

var (
	startDateFlag   string
	endDateFlag     string
	accountIDFlag   string
	limitFlag       int
	minAmountFlag   float64
	maxAmountFlag   float64
	searchQueryFlag string
	pendingOnlyFlag bool
	formatFlag      string
	outputFlag      string
	daysFlag        int
)

func init() {
	transactionsCmd.Flags().StringVar(&startDateFlag, "start-date", "", "Filter transactions starting from this date (YYYY-MM-DD)")
	transactionsCmd.Flags().StringVar(&endDateFlag, "end-date", "", "Filter transactions up to this date (YYYY-MM-DD)")
	transactionsCmd.Flags().IntVar(&daysFlag, "days", -1, "Filter transactions from the last N days (e.g. 30, 60, 90)")
	transactionsCmd.Flags().StringVar(&accountIDFlag, "account-id", "", "Filter transactions by Plaid Account ID")
	transactionsCmd.Flags().IntVar(&limitFlag, "limit", 100, "Limit the number of displayed transactions")
	transactionsCmd.Flags().Float64Var(&minAmountFlag, "min-amount", 0.0, "Filter transactions with amount greater than or equal to this")
	transactionsCmd.Flags().Float64Var(&maxAmountFlag, "max-amount", 0.0, "Filter transactions with amount less than or equal to this")
	transactionsCmd.Flags().StringVar(&searchQueryFlag, "search", "", "Search transaction name (case-insensitive)")
	transactionsCmd.Flags().BoolVar(&pendingOnlyFlag, "pending", false, "Only show pending transactions")
	transactionsCmd.Flags().StringVar(&formatFlag, "format", "table", "Output format (table/json/csv)")
	transactionsCmd.Flags().StringVar(&outputFlag, "output", "", "Output file path (default is stdout)")

	rootCmd.AddCommand(transactionsCmd)
}

var transactionsCmd = &cobra.Command{
	Use:   "transactions",
	Short: "View, search, and filter synced transaction data",
	Long:  `Query transaction records stored in your local cache. Supports sorting and extensive filtering options.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Load cache
		cache, err := config.LoadCache()
		if err != nil {
			return err
		}

		if len(cache.Transactions) == 0 {
			fmt.Fprintln(os.Stderr, "No transaction data found. Please run 'plaid-cli sync' to pull transactions first.")
			return nil
		}

		// Resolve date filtering via flags or interactive prompt
		if cmd.Flags().Changed("days") && (cmd.Flags().Changed("start-date") || cmd.Flags().Changed("end-date")) {
			return fmt.Errorf("cannot specify both --days and --start-date/--end-date flags")
		}

		if cmd.Flags().Changed("days") {
			if daysFlag <= 0 {
				return fmt.Errorf("invalid value for --days: must be a positive integer")
			}
			startDateFlag = time.Now().AddDate(0, 0, -daysFlag).Format("2006-01-02")
		} else if !cmd.Flags().Changed("start-date") && !cmd.Flags().Changed("end-date") {
			// Neither --days, --start-date, nor --end-date was specified
			// Check if we are in a terminal to prompt the user
			if isTerminal() {
				fmt.Fprintln(os.Stderr, "Filter transactions by timeframe:")
				fmt.Fprintln(os.Stderr, "  [1] Last 30 days")
				fmt.Fprintln(os.Stderr, "  [2] Last 60 days")
				fmt.Fprintln(os.Stderr, "  [3] Last 90 days")
				fmt.Fprintln(os.Stderr, "  [4] All transactions (no filter)")
				fmt.Fprint(os.Stderr, "Select an option [1-4, default 4]: ")

				reader := bufio.NewReader(os.Stdin)
				input, err := reader.ReadString('\n')
				if err == nil {
					input = strings.TrimSpace(input)
					switch input {
					case "1":
						startDateFlag = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
						fmt.Fprintln(os.Stderr, "Filtering: Last 30 days")
					case "2":
						startDateFlag = time.Now().AddDate(0, 0, -60).Format("2006-01-02")
						fmt.Fprintln(os.Stderr, "Filtering: Last 60 days")
					case "3":
						startDateFlag = time.Now().AddDate(0, 0, -90).Format("2006-01-02")
						fmt.Fprintln(os.Stderr, "Filtering: Last 90 days")
					case "4", "":
						fmt.Fprintln(os.Stderr, "Filtering: All transactions")
					default:
						fmt.Fprintln(os.Stderr, "Invalid option. Showing all transactions.")
					}
				}
			}
		}

		// 2. Apply Filters
		var filtered []plaid.Transaction
		for _, tx := range cache.Transactions {
			// Date start filter
			if startDateFlag != "" && tx.Date < startDateFlag {
				continue
			}
			// Date end filter
			if endDateFlag != "" && tx.Date > endDateFlag {
				continue
			}
			// Account filter
			if accountIDFlag != "" && tx.AccountId != accountIDFlag {
				continue
			}
			// Min Amount filter
			if cmd.Flags().Changed("min-amount") && tx.Amount < minAmountFlag {
				continue
			}
			// Max Amount filter
			if cmd.Flags().Changed("max-amount") && tx.Amount > maxAmountFlag {
				continue
			}
			// Search filter
			if searchQueryFlag != "" && !strings.Contains(strings.ToLower(tx.Name), strings.ToLower(searchQueryFlag)) {
				continue
			}
			// Pending filter
			if pendingOnlyFlag && !tx.Pending {
				continue
			}

			filtered = append(filtered, tx)
		}

		// 3. Sort by Date Descending (most recent first)
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].Date == filtered[j].Date {
				return filtered[i].TransactionId > filtered[j].TransactionId // tie breaker
			}
			return filtered[i].Date > filtered[j].Date
		})

		// 4. Apply Limit
		if limitFlag > 0 && len(filtered) > limitFlag {
			filtered = filtered[:limitFlag]
		}

		// 5. Render Output
		var outDest *os.File = os.Stdout
		if outputFlag != "" {
			var err error
			outDest, err = os.Create(outputFlag)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer outDest.Close()
		}

		switch strings.ToLower(formatFlag) {
		case "json":
			encoder := json.NewEncoder(outDest)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(filtered); err != nil {
				return fmt.Errorf("failed to encode transactions to JSON: %w", err)
			}

		case "csv":
			writer := csv.NewWriter(outDest)
			// Header
			_ = writer.Write([]string{"Transaction ID", "Account ID", "Date", "Name", "Amount", "Pending", "Category"})
			for _, tx := range filtered {
				catStr := ""
				if tx.Category != nil {
					catStr = strings.Join(tx.Category, " > ")
				}
				_ = writer.Write([]string{
					tx.TransactionId,
					tx.AccountId,
					tx.Date,
					tx.Name,
					fmt.Sprintf("%.2f", tx.Amount),
					fmt.Sprintf("%t", tx.Pending),
					catStr,
				})
			}
			writer.Flush()
			if err := writer.Error(); err != nil {
				return fmt.Errorf("csv writing error: %w", err)
			}

		case "table":
			w := tabwriter.NewWriter(outDest, 0, 0, 3, ' ', tabwriter.TabIndent)
			fmt.Fprintln(w, "DATE\tACCOUNT ID\tNAME\tAMOUNT\tPENDING\tCATEGORY")
			fmt.Fprintln(w, "----\t----------\t----\t------\t-------\t--------")
			for _, tx := range filtered {
				catStr := "-"
				if len(tx.Category) > 0 {
					catStr = tx.Category[0]
					if len(tx.Category) > 1 {
						catStr += " (" + tx.Category[1] + ")"
					}
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%.2f\t%t\t%s\n",
					tx.Date,
					tx.AccountId[:8]+"...[truncated]", // Truncate long ID for layout
					tx.Name,
					tx.Amount,
					tx.Pending,
					catStr,
				)
			}
			_ = w.Flush()

		default:
			return fmt.Errorf("unsupported output format '%s'. Choose table, json, or csv", formatFlag)
		}

		if outputFlag != "" {
			fmt.Fprintf(os.Stderr, "Successfully exported %d transactions to %s\n", len(filtered), outputFlag)
		}

		return nil
	},
}

func isTerminal() bool {
	if os.Getenv("PLAID_CLI_TEST_TERMINAL") == "true" {
		return true
	}
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
