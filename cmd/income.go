package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"plaid-cli/pkg/server"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/plaid/plaid-go/v43/plaid"
	"github.com/spf13/cobra"
)

var (
	incomePortFlag   int
	incomeFormatFlag string
	incomeOutputFlag string
)

func init() {
	incomeLinkCmd.Flags().IntVar(&incomePortFlag, "port", 8080, "Local port to spin up the Plaid Link flow page")
	incomePaystubsCmd.Flags().StringVar(&incomeFormatFlag, "format", "table", "Output format (table/json/csv)")
	incomePaystubsCmd.Flags().StringVar(&incomeOutputFlag, "output", "", "Output file path (default is stdout)")

	incomeCmd.AddCommand(incomeLinkCmd)
	incomeCmd.AddCommand(incomePaystubsCmd)
	rootCmd.AddCommand(incomeCmd)
}

var incomeCmd = &cobra.Command{
	Use:   "income",
	Short: "Verify payroll income and retrieve pay stubs (ADP and other payroll providers)",
	Long: `Connect to a payroll provider (ADP, Workday, Gusto, and ~80% of US payroll
providers) via Plaid's Payroll Income product and retrieve structured pay stub data.

This is a separate product from Transactions/Liabilities/Investments: it is scoped
to a Plaid user_id rather than a bank Item's access_token, so it requires its own
'income link' flow before 'income paystubs' can fetch data.

Note: Payroll Income is not included in Plaid's Trial plan. Sandbox testing works
regardless of your plan; retrieving real data in production requires upgrading.`,
}

// ---- income link ----

var incomeLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Authenticate with a payroll provider using Plaid Link",
	Long: `Start a temporary local server and open your browser to connect a payroll
provider (e.g. ADP) via Plaid Link. On completion, waits for Plaid to finish
processing the connection, then saves the user_id needed by 'income paystubs'.

Safe to run again later to reconnect or add another payroll provider; the same
user_id is reused.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		plaidClient, err := client.NewPlaidClient(cfg)
		if err != nil {
			return err
		}

		// A user_id is created once per plaid-cli user and reused across
		// every income link — unlike bank Items, Income data isn't scoped per
		// connection, so there's nothing to gain from minting a fresh one.
		if cfg.UserID == "" {
			fmt.Println("Creating Plaid user...")
			userID, err := client.CreateUserID(plaidClient)
			if err != nil {
				return fmt.Errorf("failed to create Plaid user: %w", err)
			}
			cfg.UserID = userID
			if err := cfg.SaveConfig(); err != nil {
				return fmt.Errorf("failed to save user id: %w", err)
			}
		}

		fmt.Println("Generating Plaid Link token for Payroll Income...")
		linkToken, err := client.CreateIncomeLinkToken(plaidClient, cfg.UserID)
		if err != nil {
			return fmt.Errorf("failed to create income link token: %w", err)
		}

		url := fmt.Sprintf("http://localhost:%d", incomePortFlag)
		fmt.Printf("Starting local authentication server on %s ...\n", url)
		fmt.Println("Opening web browser to connect your payroll provider (e.g. ADP)...")

		go func() {
			_ = openBrowser(url)
		}()

		subtitle := "Please complete authentication using Plaid Link to connect your payroll provider (e.g. ADP) securely."
		// The public token is intentionally ignored: Payroll Income data is
		// retrieved via the user_id-scoped /credit/payroll_income/get, not
		// through an Item access_token exchange.
		if _, err := server.StartServer(incomePortFlag, linkToken, subtitle); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		fmt.Println("\nAuthentication completed by browser!")
		fmt.Println("Waiting for Plaid to finish processing your payroll connection (this can take a couple of minutes)...")

		resp, err := client.PollPayrollIncome(plaidClient, cfg.UserID, 3*time.Minute)
		if err != nil {
			return fmt.Errorf("income verification did not complete: %w", err)
		}

		items := resp.GetItems()
		fmt.Printf("\nSuccess! Retrieved data from %d payroll connection(s):\n", len(items))
		for _, item := range items {
			itemStatus := item.GetStatus()
			numStubs := len(item.GetPayrollIncome())
			fmt.Printf("  - %s: status=%s, %d payroll record(s)\n", item.GetInstitutionName(), itemStatus.GetProcessingStatus(), numStubs)
		}
		fmt.Println("\nRun 'plaid-cli income paystubs' to view the retrieved pay stubs.")
		return nil
	},
}

// ---- income paystubs ----

type paystubRow struct {
	Institution      string   `json:"institution"`
	Employer         string   `json:"employer"`
	PayDate          string   `json:"pay_date"`
	PayPeriodStart   string   `json:"pay_period_start"`
	PayPeriodEnd     string   `json:"pay_period_end"`
	PayFrequency     string   `json:"pay_frequency"`
	GrossEarnings    *float64 `json:"gross_earnings"`
	TotalDeductions  *float64 `json:"total_deductions"`
	NetPay           *float64 `json:"net_pay"`
	YtdGrossEarnings *float64 `json:"ytd_gross_earnings"`
	YtdNetPay        *float64 `json:"ytd_net_pay"`
}

var incomePaystubsCmd = &cobra.Command{
	Use:   "paystubs",
	Short: "Display retrieved pay stub data",
	Long: `Fetch and display pay stub data (gross/net pay, deductions, pay period, and
year-to-date totals) retrieved from connected payroll providers via
/credit/payroll_income/get.

Requires 'plaid-cli income link' to have been run first.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}
		if cfg.UserID == "" {
			return fmt.Errorf("no payroll connection found. Run 'plaid-cli income link' first")
		}

		plaidClient, err := client.NewPlaidClient(cfg)
		if err != nil {
			return err
		}

		fmt.Fprintln(os.Stderr, "Fetching payroll income...")
		resp, err := client.FetchPayrollIncome(plaidClient, cfg.UserID)
		if err != nil {
			return fmt.Errorf("failed to fetch payroll income: %w", err)
		}

		var rows []paystubRow
		for _, item := range resp.GetItems() {
			for _, po := range item.GetPayrollIncome() {
				for _, stub := range po.GetPayStubs() {
					rows = append(rows, paystubRowFrom(item.GetInstitutionName(), stub))
				}
			}
		}

		if len(rows) == 0 {
			fmt.Fprintln(os.Stderr, "No pay stubs found.")
			return nil
		}

		var outDest *os.File = os.Stdout
		if incomeOutputFlag != "" {
			outDest, err = os.Create(incomeOutputFlag)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer outDest.Close()
		}

		switch strings.ToLower(incomeFormatFlag) {
		case "json":
			encoder := json.NewEncoder(outDest)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(rows); err != nil {
				return fmt.Errorf("failed to encode pay stubs to JSON: %w", err)
			}
		case "csv":
			if err := writePaystubsCSV(outDest, rows); err != nil {
				return err
			}
		case "table":
			writePaystubsTable(outDest, rows)
		default:
			return fmt.Errorf("unsupported output format '%s'. Choose table, json, or csv", incomeFormatFlag)
		}

		if incomeOutputFlag != "" {
			fmt.Fprintf(os.Stderr, "Successfully exported pay stubs to %s\n", incomeOutputFlag)
		}
		return nil
	},
}

func paystubRowFrom(institutionName string, s plaid.CreditPayStub) paystubRow {
	period := s.PayPeriodDetails
	earningsTotal := s.Earnings.Total
	deductionsTotal := s.Deductions.Total
	netPay := s.NetPay

	return paystubRow{
		Institution:      institutionName,
		Employer:         lsStr(s.Employer.Name),
		PayDate:          lsStr(period.PayDate),
		PayPeriodStart:   lsStr(period.StartDate),
		PayPeriodEnd:     lsStr(period.EndDate),
		PayFrequency:     lsStr(period.PayFrequency),
		GrossEarnings:    lsFloat(earningsTotal.CurrentAmount),
		TotalDeductions:  lsFloat(deductionsTotal.CurrentAmount),
		NetPay:           lsFloat(netPay.CurrentAmount),
		YtdGrossEarnings: lsFloat(earningsTotal.YtdAmount),
		YtdNetPay:        lsFloat(netPay.YtdAmount),
	}
}

func writePaystubsTable(w *os.File, rows []paystubRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', tabwriter.TabIndent)
	fmt.Fprintln(tw, "EMPLOYER\tPAY DATE\tPERIOD\tGROSS\tDEDUCTIONS\tNET PAY\tYTD GROSS\tYTD NET")
	fmt.Fprintln(tw, "--------\t--------\t------\t-----\t----------\t-------\t---------\t-------")
	for _, r := range rows {
		period := fmt.Sprintf("%s - %s", dash(r.PayPeriodStart), dash(r.PayPeriodEnd))
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			dash(r.Employer),
			dash(r.PayDate),
			period,
			money(r.GrossEarnings),
			money(r.TotalDeductions),
			money(r.NetPay),
			money(r.YtdGrossEarnings),
			money(r.YtdNetPay),
		)
	}
	_ = tw.Flush()
}

func writePaystubsCSV(w *os.File, rows []paystubRow) error {
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"Institution", "Employer", "Pay Date", "Period Start", "Period End", "Pay Frequency",
		"Gross Earnings", "Total Deductions", "Net Pay", "YTD Gross Earnings", "YTD Net Pay",
	})
	for _, r := range rows {
		_ = writer.Write([]string{
			r.Institution, r.Employer, r.PayDate, r.PayPeriodStart, r.PayPeriodEnd, r.PayFrequency,
			moneyRaw(r.GrossEarnings), moneyRaw(r.TotalDeductions), moneyRaw(r.NetPay),
			moneyRaw(r.YtdGrossEarnings), moneyRaw(r.YtdNetPay),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("csv writing error: %w", err)
	}
	return nil
}
