package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/plaid/plaid-go/v20/plaid"
	"github.com/spf13/cobra"
)

var (
	invItemIDFlag  string
	invAccountFlag string
	invFormatFlag  string
	invOutputFlag  string

	// transactions-only flags
	invStartDateFlag string
	invEndDateFlag   string
	invDaysFlag      int
	invTypeFlag      string
	invLimitFlag     int
)

func init() {
	// Shared flags on both subcommands.
	for _, c := range []*cobra.Command{investmentsHoldingsCmd, investmentsTransactionsCmd} {
		c.Flags().StringVar(&invItemIDFlag, "item-id", "", "Limit to a single Plaid Item ID")
		c.Flags().StringVar(&invAccountFlag, "account-id", "", "Limit to a single Plaid Account ID")
		c.Flags().StringVar(&invFormatFlag, "format", "table", "Output format (table/json/csv)")
		c.Flags().StringVar(&invOutputFlag, "output", "", "Output file path (default is stdout)")
	}

	investmentsTransactionsCmd.Flags().StringVar(&invStartDateFlag, "start-date", "", "Earliest transaction date (YYYY-MM-DD)")
	investmentsTransactionsCmd.Flags().StringVar(&invEndDateFlag, "end-date", "", "Latest transaction date (YYYY-MM-DD)")
	investmentsTransactionsCmd.Flags().IntVar(&invDaysFlag, "days", -1, "Fetch transactions from the last N days (mutually exclusive with --start-date/--end-date)")
	investmentsTransactionsCmd.Flags().StringVar(&invTypeFlag, "type", "", "Filter by investment transaction type or subtype (e.g. buy, sell, dividend, fee)")
	investmentsTransactionsCmd.Flags().IntVar(&invLimitFlag, "limit", 100, "Limit the number of displayed transactions")

	investmentsCmd.AddCommand(investmentsHoldingsCmd)
	investmentsCmd.AddCommand(investmentsTransactionsCmd)
	rootCmd.AddCommand(investmentsCmd)
}

var investmentsCmd = &cobra.Command{
	Use:   "investments",
	Short: "View brokerage holdings and investment transactions",
	Long:  `Inspect investment accounts: current holdings (positions) and investment activity (buys, sells, dividends, fees). Data is fetched live from Plaid.`,
}

// ---- holdings ----

type holdingRow struct {
	Account     string   `json:"account"`
	AccountID   string   `json:"account_id"`
	Security    string   `json:"security"`
	Ticker      string   `json:"ticker"`
	Type        string   `json:"type"`
	Quantity    float64  `json:"quantity"`
	Price       float64  `json:"price"`
	Value       float64  `json:"value"`
	CostBasis   *float64 `json:"cost_basis"`
	GainLoss    *float64 `json:"gain_loss"`
	GainLossPct *float64 `json:"gain_loss_pct"`
}

var investmentsHoldingsCmd = &cobra.Command{
	Use:   "holdings",
	Short: "Display current investment holdings (positions)",
	Long:  `Fetch current positions across all linked brokerage accounts, with market value and unrealized gain/loss computed from cost basis.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		plaidClient, targetItems, err := investmentsSetup()
		if err != nil {
			return err
		}

		fmt.Fprintln(os.Stderr, "Fetching holdings...")
		var rows []holdingRow
		acctDir := map[string]config.Account{}
		fetched := 0
		for idx, item := range targetItems {
			fmt.Fprintf(os.Stderr, "[%d/%d] Fetching holdings for Item %s...\n", idx+1, len(targetItems), config.ShortAccountID(item.ItemID))
			resp, err := client.FetchHoldings(plaidClient, item.AccessToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to fetch holdings for Item %s: %v. Skipping.\n", item.ItemID, err)
				continue
			}
			fetched++

			for _, acc := range resp.Accounts {
				acctDir[acc.AccountId] = accountMetaFrom(acc)
			}
			secMap := securityMap(resp.Securities)
			for _, h := range resp.Holdings {
				rows = append(rows, holdingRowFrom(h, secMap, acctDir))
			}
		}

		if fetched == 0 {
			return fmt.Errorf("could not fetch holdings for any linked item")
		}

		if invAccountFlag != "" {
			kept := rows[:0]
			for _, r := range rows {
				if r.AccountID == invAccountFlag {
					kept = append(kept, r)
				}
			}
			rows = kept
		}

		if len(rows) == 0 {
			fmt.Fprintln(os.Stderr, "No holdings found.")
			return nil
		}

		sort.Slice(rows, func(i, j int) bool { return rows[i].Value > rows[j].Value })

		outDest, cleanup, err := investmentsOutput()
		if err != nil {
			return err
		}
		defer cleanup()

		switch strings.ToLower(invFormatFlag) {
		case "json":
			enc := json.NewEncoder(outDest)
			enc.SetIndent("", "  ")
			if err := enc.Encode(rows); err != nil {
				return fmt.Errorf("failed to encode holdings to JSON: %w", err)
			}
		case "csv":
			w := csv.NewWriter(outDest)
			_ = w.Write([]string{"Account", "Account ID", "Security", "Ticker", "Type", "Quantity", "Price", "Value", "Cost Basis", "Gain/Loss", "Gain/Loss %"})
			for _, r := range rows {
				_ = w.Write([]string{
					r.Account, r.AccountID, r.Security, r.Ticker, r.Type,
					fmt.Sprintf("%.4f", r.Quantity), fmt.Sprintf("%.2f", r.Price), fmt.Sprintf("%.2f", r.Value),
					moneyRaw(r.CostBasis), moneyRaw(r.GainLoss), pctRaw(r.GainLossPct),
				})
			}
			w.Flush()
			if err := w.Error(); err != nil {
				return fmt.Errorf("csv writing error: %w", err)
			}
		case "table":
			writeHoldingsTable(outDest, rows)
		default:
			return fmt.Errorf("unsupported output format '%s'. Choose table, json, or csv", invFormatFlag)
		}

		if invOutputFlag != "" {
			fmt.Fprintf(os.Stderr, "Successfully exported %d holdings to %s\n", len(rows), invOutputFlag)
		}
		return nil
	},
}

func holdingRowFrom(h plaid.Holding, secMap map[string]plaid.Security, dir map[string]config.Account) holdingRow {
	name, ticker, typ := securityLabel(secMap, h.SecurityId)
	row := holdingRow{
		Account:   config.AccountLabelFrom(dir, h.AccountId),
		AccountID: h.AccountId,
		Security:  name,
		Ticker:    ticker,
		Type:      typ,
		Quantity:  h.Quantity,
		Price:     h.InstitutionPrice,
		Value:     h.InstitutionValue,
		CostBasis: lsFloat(h.CostBasis),
	}
	if row.CostBasis != nil {
		gl := h.InstitutionValue - *row.CostBasis
		row.GainLoss = &gl
		if *row.CostBasis != 0 {
			pct := gl / *row.CostBasis * 100
			row.GainLossPct = &pct
		}
	}
	return row
}

func writeHoldingsTable(w *os.File, rows []holdingRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', tabwriter.TabIndent)
	fmt.Fprintln(tw, "SECURITY\tTICKER\tQTY\tPRICE\tVALUE\tCOST BASIS\tGAIN/LOSS")
	fmt.Fprintln(tw, "--------\t------\t---\t-----\t-----\t----------\t---------")
	var totalValue, totalCost float64
	hasCost := false
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Security,
			dash(r.Ticker),
			trimFloat(r.Quantity),
			usd(r.Price),
			usd(r.Value),
			money(r.CostBasis),
			gainLossStr(r.GainLoss, r.GainLossPct),
		)
		totalValue += r.Value
		if r.CostBasis != nil {
			totalCost += *r.CostBasis
			hasCost = true
		}
	}
	// Totals row.
	var totalGL, totalPct *float64
	if hasCost {
		gl := totalValue - totalCost
		totalGL = &gl
		if totalCost != 0 {
			pct := gl / totalCost * 100
			totalPct = &pct
		}
	}
	tc := (*float64)(nil)
	if hasCost {
		tc = &totalCost
	}
	fmt.Fprintln(tw, "\t\t\t\t\t\t")
	fmt.Fprintf(tw, "TOTAL\t\t\t\t%s\t%s\t%s\n", usd(totalValue), money(tc), gainLossStr(totalGL, totalPct))
	_ = tw.Flush()
}

// ---- transactions ----

type investmentTxnRow struct {
	Date      string   `json:"date"`
	Account   string   `json:"account"`
	AccountID string   `json:"account_id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	Ticker    string   `json:"ticker"`
	Quantity  float64  `json:"quantity"`
	Price     float64  `json:"price"`
	Amount    float64  `json:"amount"`
	Fees      *float64 `json:"fees"`
}

var investmentsTransactionsCmd = &cobra.Command{
	Use:   "transactions",
	Short: "Display investment activity (buys, sells, dividends, fees)",
	Long:  `Fetch investment transactions within a date window. The date window defaults to the last 365 days.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("days") && (cmd.Flags().Changed("start-date") || cmd.Flags().Changed("end-date")) {
			return fmt.Errorf("cannot specify both --days and --start-date/--end-date")
		}

		// Resolve the date window.
		endDate := invEndDateFlag
		if endDate == "" {
			endDate = time.Now().Format("2006-01-02")
		}
		startDate := invStartDateFlag
		if cmd.Flags().Changed("days") {
			if invDaysFlag <= 0 {
				return fmt.Errorf("invalid value for --days: must be a positive integer")
			}
			startDate = time.Now().AddDate(0, 0, -invDaysFlag).Format("2006-01-02")
		}
		if startDate == "" {
			startDate = time.Now().AddDate(0, 0, -365).Format("2006-01-02")
		}

		plaidClient, targetItems, err := investmentsSetup()
		if err != nil {
			return err
		}

		typeFilter := strings.ToLower(strings.TrimSpace(invTypeFlag))

		fmt.Fprintf(os.Stderr, "Fetching investment transactions (%s to %s)...\n", startDate, endDate)
		var rows []investmentTxnRow
		acctDir := map[string]config.Account{}
		fetched := 0
		for idx, item := range targetItems {
			fmt.Fprintf(os.Stderr, "[%d/%d] Fetching transactions for Item %s...\n", idx+1, len(targetItems), config.ShortAccountID(item.ItemID))
			txns, securities, accounts, err := client.FetchInvestmentTransactions(plaidClient, item.AccessToken, startDate, endDate)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to fetch investment transactions for Item %s: %v. Skipping.\n", item.ItemID, err)
				continue
			}
			fetched++

			for _, acc := range accounts {
				acctDir[acc.AccountId] = accountMetaFrom(acc)
			}
			secMap := securityMap(securities)
			for _, t := range txns {
				rows = append(rows, investmentTxnRowFrom(t, secMap, acctDir))
			}
		}

		if fetched == 0 {
			return fmt.Errorf("could not fetch investment transactions for any linked item")
		}

		// Filters: account, type/subtype.
		filtered := rows[:0]
		for _, r := range rows {
			if invAccountFlag != "" && r.AccountID != invAccountFlag {
				continue
			}
			if typeFilter != "" && !strings.EqualFold(r.Type, typeFilter) && !strings.EqualFold(r.Subtype, typeFilter) {
				continue
			}
			filtered = append(filtered, r)
		}
		rows = filtered

		if len(rows) == 0 {
			fmt.Fprintln(os.Stderr, "No investment transactions found.")
			return nil
		}

		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Date == rows[j].Date {
				return rows[i].Name < rows[j].Name
			}
			return rows[i].Date > rows[j].Date
		})

		if invLimitFlag > 0 && len(rows) > invLimitFlag {
			rows = rows[:invLimitFlag]
		}

		outDest, cleanup, err := investmentsOutput()
		if err != nil {
			return err
		}
		defer cleanup()

		switch strings.ToLower(invFormatFlag) {
		case "json":
			enc := json.NewEncoder(outDest)
			enc.SetIndent("", "  ")
			if err := enc.Encode(rows); err != nil {
				return fmt.Errorf("failed to encode investment transactions to JSON: %w", err)
			}
		case "csv":
			w := csv.NewWriter(outDest)
			_ = w.Write([]string{"Date", "Account", "Account ID", "Name", "Type", "Subtype", "Ticker", "Quantity", "Price", "Amount", "Fees"})
			for _, r := range rows {
				_ = w.Write([]string{
					r.Date, r.Account, r.AccountID, r.Name, r.Type, r.Subtype, r.Ticker,
					fmt.Sprintf("%.4f", r.Quantity), fmt.Sprintf("%.2f", r.Price), fmt.Sprintf("%.2f", r.Amount), moneyRaw(r.Fees),
				})
			}
			w.Flush()
			if err := w.Error(); err != nil {
				return fmt.Errorf("csv writing error: %w", err)
			}
		case "table":
			tw := tabwriter.NewWriter(outDest, 0, 0, 3, ' ', tabwriter.TabIndent)
			fmt.Fprintln(tw, "DATE\tNAME\tTYPE\tTICKER\tQTY\tPRICE\tAMOUNT")
			fmt.Fprintln(tw, "----\t----\t----\t------\t---\t-----\t------")
			for _, r := range rows {
				typeStr := r.Type
				if r.Subtype != "" && !strings.EqualFold(r.Subtype, r.Type) {
					typeStr = fmt.Sprintf("%s/%s", r.Type, r.Subtype)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					r.Date, r.Name, typeStr, dash(r.Ticker),
					trimFloat(r.Quantity), usd(r.Price), usd(r.Amount),
				)
			}
			_ = tw.Flush()
		default:
			return fmt.Errorf("unsupported output format '%s'. Choose table, json, or csv", invFormatFlag)
		}

		if invOutputFlag != "" {
			fmt.Fprintf(os.Stderr, "Successfully exported %d investment transactions to %s\n", len(rows), invOutputFlag)
		}
		return nil
	},
}

func investmentTxnRowFrom(t plaid.InvestmentTransaction, secMap map[string]plaid.Security, dir map[string]config.Account) investmentTxnRow {
	_, ticker, _ := securityLabel(secMap, lsStr(t.SecurityId))
	return investmentTxnRow{
		Date:      t.Date,
		Account:   config.AccountLabelFrom(dir, t.AccountId),
		AccountID: t.AccountId,
		Name:      t.Name,
		Type:      string(t.Type),
		Subtype:   string(t.Subtype),
		Ticker:    ticker,
		Quantity:  t.Quantity,
		Price:     t.Price,
		Amount:    t.Amount,
		Fees:      lsFloat(t.Fees),
	}
}

// ---- shared helpers ----

// investmentsSetup loads config, builds the Plaid client, and resolves the set of
// items to query (all, or a single --item-id).
func investmentsSetup() (*plaid.APIClient, []config.LinkedItem, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, nil, err
	}
	if len(cfg.Items) == 0 {
		return nil, nil, fmt.Errorf("no accounts linked. Please run 'plaid-cli login' first")
	}
	plaidClient, err := client.NewPlaidClient(cfg)
	if err != nil {
		return nil, nil, err
	}

	targetItems := cfg.Items
	if invItemIDFlag != "" {
		var matched []config.LinkedItem
		for _, item := range cfg.Items {
			if item.ItemID == invItemIDFlag {
				matched = append(matched, item)
				break
			}
		}
		if len(matched) == 0 {
			return nil, nil, fmt.Errorf("item ID %s not found in linked accounts config", invItemIDFlag)
		}
		targetItems = matched
	}
	return plaidClient, targetItems, nil
}

// investmentsOutput resolves the output destination (stdout or --output file) and
// returns a cleanup func to close the file when one was opened.
func investmentsOutput() (*os.File, func(), error) {
	if invOutputFlag == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(invOutputFlag)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create output file: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

func securityMap(securities []plaid.Security) map[string]plaid.Security {
	m := make(map[string]plaid.Security, len(securities))
	for _, s := range securities {
		m[s.SecurityId] = s
	}
	return m
}

// securityLabel resolves a security_id to a display name, ticker, and type,
// falling back to the short ID when the security is unknown or unnamed.
func securityLabel(secMap map[string]plaid.Security, id string) (name, ticker, typ string) {
	if id == "" {
		return "", "", ""
	}
	s, ok := secMap[id]
	if !ok {
		return config.ShortAccountID(id), "", ""
	}
	name = lsStr(s.Name)
	ticker = lsStr(s.TickerSymbol)
	typ = lsStr(s.Type)
	if name == "" {
		if ticker != "" {
			name = ticker
		} else {
			name = config.ShortAccountID(id)
		}
	}
	return name, ticker, typ
}

// usd renders a plain (non-nullable) dollar amount.
func usd(f float64) string {
	return "$" + fmt.Sprintf("%.2f", f)
}

// trimFloat renders a quantity without trailing zeros (e.g. 12.5, 10, 1.2345).
func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", f), "0"), ".")
}

// gainLossStr renders an unrealized gain/loss with sign and optional percentage,
// e.g. "+$326.63 (+11.3%)" or "-$50.00 (-2.1%)"; "-" when unknown.
func gainLossStr(gl, pct *float64) string {
	if gl == nil {
		return "-"
	}
	sign := "+"
	abs := *gl
	if abs < 0 {
		sign = "-"
		abs = -abs
	}
	s := fmt.Sprintf("%s$%.2f", sign, abs)
	if pct != nil {
		p := *pct
		if p < 0 {
			p = -p
		}
		s += fmt.Sprintf(" (%s%.1f%%)", sign, p)
	}
	return s
}

// pctRaw renders a nullable percentage as a bare number for CSV, "" when absent.
func pctRaw(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *v)
}
