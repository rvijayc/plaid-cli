package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"strings"
	"text/tabwriter"

	"github.com/plaid/plaid-go/v43/plaid"
	"github.com/spf13/cobra"
)

var (
	liabTypeFlag    string
	liabItemIDFlag  string
	liabAccountFlag string
	liabFormatFlag  string
	liabOutputFlag  string
)

func init() {
	liabilitiesCmd.Flags().StringVar(&liabTypeFlag, "type", "", "Show only one liability class (credit, student, or mortgage)")
	liabilitiesCmd.Flags().StringVar(&liabItemIDFlag, "item-id", "", "Limit to a single Plaid Item ID")
	liabilitiesCmd.Flags().StringVar(&liabAccountFlag, "account-id", "", "Limit to a single Plaid Account ID")
	liabilitiesCmd.Flags().StringVar(&liabFormatFlag, "format", "table", "Output format (table/json/csv)")
	liabilitiesCmd.Flags().StringVar(&liabOutputFlag, "output", "", "Output file path (default is stdout)")
	rootCmd.AddCommand(liabilitiesCmd)
}

// Liability display rows. These flatten the relevant Plaid fields into a single
// struct per account so the table/json/csv renderers share one representation.
type creditRow struct {
	Account              string   `json:"account"`
	AccountID            string   `json:"account_id"`
	LastStatementBalance *float64 `json:"last_statement_balance"`
	LastStatementDate    string   `json:"last_statement_date"`
	MinimumPayment       *float64 `json:"minimum_payment_amount"`
	NextPaymentDueDate   string   `json:"next_payment_due_date"`
	LastPaymentAmount    *float64 `json:"last_payment_amount"`
	LastPaymentDate      string   `json:"last_payment_date"`
	PurchaseAPR          *float64 `json:"purchase_apr"`
	IsOverdue            *bool    `json:"is_overdue"`
}

type studentRow struct {
	Account             string   `json:"account"`
	AccountID           string   `json:"account_id"`
	InterestRate        float64  `json:"interest_rate_percentage"`
	OutstandingInterest *float64 `json:"outstanding_interest_amount"`
	MinimumPayment      *float64 `json:"minimum_payment_amount"`
	NextPaymentDueDate  string   `json:"next_payment_due_date"`
	ExpectedPayoffDate  string   `json:"expected_payoff_date"`
	LoanStatus          string   `json:"loan_status"`
	RepaymentPlan       string   `json:"repayment_plan"`
}

type mortgageRow struct {
	Account              string   `json:"account"`
	AccountID            string   `json:"account_id"`
	InterestRate         *float64 `json:"interest_rate_percentage"`
	InterestRateType     string   `json:"interest_rate_type"`
	NextMonthlyPayment   *float64 `json:"next_monthly_payment"`
	NextPaymentDueDate   string   `json:"next_payment_due_date"`
	OriginationPrincipal *float64 `json:"origination_principal_amount"`
	MaturityDate         string   `json:"maturity_date"`
	YtdInterestPaid      *float64 `json:"ytd_interest_paid"`
	YtdPrincipalPaid     *float64 `json:"ytd_principal_paid"`
	EscrowBalance        *float64 `json:"escrow_balance"`
}

type liabilitiesOutput struct {
	Credit   []creditRow   `json:"credit,omitempty"`
	Student  []studentRow  `json:"student,omitempty"`
	Mortgage []mortgageRow `json:"mortgage,omitempty"`
}

var liabilitiesCmd = &cobra.Command{
	Use:   "liabilities",
	Short: "View credit card, student loan, and mortgage liability detail",
	Long: `Fetch liability detail (statement balances, APRs, due dates, interest rates, and
payoff projections) for all linked items that carry a credit or loan account.

Liabilities are fetched live from Plaid; items with no liability accounts, or whose
institution does not support the Liabilities product, are skipped.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Validate --type up front.
		liabType := strings.ToLower(strings.TrimSpace(liabTypeFlag))
		switch liabType {
		case "", "credit", "student", "mortgage":
		default:
			return fmt.Errorf("invalid --type %q: choose credit, student, or mortgage", liabTypeFlag)
		}

		// 2. Load configuration.
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}
		if len(cfg.Items) == 0 {
			return fmt.Errorf("no accounts linked. Please run 'plaid-cli login' first")
		}

		// 3. Initialize Plaid API client.
		plaidClient, err := client.NewPlaidClient(cfg)
		if err != nil {
			return err
		}

		// 4. Resolve target items.
		targetItems := cfg.Items
		if liabItemIDFlag != "" {
			var matched []config.LinkedItem
			for _, item := range cfg.Items {
				if item.ItemID == liabItemIDFlag {
					matched = append(matched, item)
					break
				}
			}
			if len(matched) == 0 {
				return fmt.Errorf("item ID %s not found in linked accounts config", liabItemIDFlag)
			}
			targetItems = matched
		}

		// 5. Fetch liabilities per item, accumulating display rows.
		fmt.Fprintln(os.Stderr, "Fetching liabilities...")
		var out liabilitiesOutput
		acctDir := map[string]config.Account{}
		fetched := 0
		for idx, item := range targetItems {
			label := config.ShortAccountID(item.ItemID)
			fmt.Fprintf(os.Stderr, "[%d/%d] Fetching liabilities for Item %s...\n", idx+1, len(targetItems), label)

			resp, err := client.FetchLiabilities(plaidClient, item.AccessToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to fetch liabilities for Item %s: %v. Skipping.\n", item.ItemID, err)
				continue
			}
			fetched++

			// Build an account directory from the response so rows render labels.
			for _, acc := range resp.Accounts {
				acctDir[acc.AccountId] = accountMetaFrom(acc)
			}

			liab := resp.Liabilities
			for _, c := range liab.GetCredit() {
				out.Credit = append(out.Credit, creditRowFrom(c, acctDir))
			}
			for _, s := range liab.GetStudent() {
				out.Student = append(out.Student, studentRowFrom(s, acctDir))
			}
			for _, m := range liab.GetMortgage() {
				out.Mortgage = append(out.Mortgage, mortgageRowFrom(m, acctDir))
			}
		}

		if fetched == 0 {
			return fmt.Errorf("could not fetch liabilities for any linked item")
		}

		// 6. Apply --account-id and --type filters.
		out = filterLiabilities(out, liabAccountFlag, liabType)

		if len(out.Credit) == 0 && len(out.Student) == 0 && len(out.Mortgage) == 0 {
			fmt.Fprintln(os.Stderr, "No liability accounts found.")
			return nil
		}

		// 7. Render output.
		var outDest *os.File = os.Stdout
		if liabOutputFlag != "" {
			outDest, err = os.Create(liabOutputFlag)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer outDest.Close()
		}

		switch strings.ToLower(liabFormatFlag) {
		case "json":
			encoder := json.NewEncoder(outDest)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(out); err != nil {
				return fmt.Errorf("failed to encode liabilities to JSON: %w", err)
			}
		case "csv":
			if err := writeLiabilitiesCSV(outDest, out); err != nil {
				return err
			}
		case "table":
			writeLiabilitiesTable(outDest, out)
		default:
			return fmt.Errorf("unsupported output format '%s'. Choose table, json, or csv", liabFormatFlag)
		}

		if liabOutputFlag != "" {
			fmt.Fprintf(os.Stderr, "Successfully exported liabilities to %s\n", liabOutputFlag)
		}
		return nil
	},
}

// --- row builders ---

func creditRowFrom(c plaid.CreditCardLiability, dir map[string]config.Account) creditRow {
	id := lsStr(c.AccountId)
	row := creditRow{
		Account:              config.AccountLabelFrom(dir, id),
		AccountID:            id,
		LastStatementBalance: lsFloat(c.LastStatementBalance),
		LastStatementDate:    lsStr(c.LastStatementIssueDate),
		MinimumPayment:       lsFloat(c.MinimumPaymentAmount),
		NextPaymentDueDate:   lsStr(c.NextPaymentDueDate),
		LastPaymentAmount:    lsFloat(c.LastPaymentAmount),
		LastPaymentDate:      lsStr(c.LastPaymentDate),
		IsOverdue:            lsBool(c.IsOverdue),
	}
	// Surface the purchase APR if present (most relevant of the APR set).
	for _, apr := range c.Aprs {
		if strings.EqualFold(apr.AprType, "purchase_apr") {
			p := apr.AprPercentage
			row.PurchaseAPR = &p
			break
		}
	}
	return row
}

func studentRowFrom(s plaid.StudentLoan, dir map[string]config.Account) studentRow {
	id := lsStr(s.AccountId)
	return studentRow{
		Account:             config.AccountLabelFrom(dir, id),
		AccountID:           id,
		InterestRate:        s.InterestRatePercentage,
		OutstandingInterest: lsFloat(s.OutstandingInterestAmount),
		MinimumPayment:      lsFloat(s.MinimumPaymentAmount),
		NextPaymentDueDate:  lsStr(s.NextPaymentDueDate),
		ExpectedPayoffDate:  lsStr(s.ExpectedPayoffDate),
		LoanStatus:          lsStr(s.LoanStatus.Type),
		RepaymentPlan:       lsStr(s.RepaymentPlan.Type),
	}
}

func mortgageRowFrom(m plaid.MortgageLiability, dir map[string]config.Account) mortgageRow {
	return mortgageRow{
		Account:              config.AccountLabelFrom(dir, m.AccountId),
		AccountID:            m.AccountId,
		InterestRate:         lsFloat(m.InterestRate.Percentage),
		InterestRateType:     lsStr(m.InterestRate.Type),
		NextMonthlyPayment:   lsFloat(m.NextMonthlyPayment),
		NextPaymentDueDate:   lsStr(m.NextPaymentDueDate),
		OriginationPrincipal: lsFloat(m.OriginationPrincipalAmount),
		MaturityDate:         lsStr(m.MaturityDate),
		YtdInterestPaid:      lsFloat(m.YtdInterestPaid),
		YtdPrincipalPaid:     lsFloat(m.YtdPrincipalPaid),
		EscrowBalance:        lsFloat(m.EscrowBalance),
	}
}

// filterLiabilities narrows the output by account ID and/or liability type.
func filterLiabilities(out liabilitiesOutput, accountID, liabType string) liabilitiesOutput {
	if accountID != "" {
		credit := out.Credit[:0]
		for _, r := range out.Credit {
			if r.AccountID == accountID {
				credit = append(credit, r)
			}
		}
		out.Credit = credit
		student := out.Student[:0]
		for _, r := range out.Student {
			if r.AccountID == accountID {
				student = append(student, r)
			}
		}
		out.Student = student
		mortgage := out.Mortgage[:0]
		for _, r := range out.Mortgage {
			if r.AccountID == accountID {
				mortgage = append(mortgage, r)
			}
		}
		out.Mortgage = mortgage
	}

	switch liabType {
	case "credit":
		out.Student, out.Mortgage = nil, nil
	case "student":
		out.Credit, out.Mortgage = nil, nil
	case "mortgage":
		out.Credit, out.Student = nil, nil
	}
	return out
}

// --- renderers ---

func writeLiabilitiesTable(w *os.File, out liabilitiesOutput) {
	if len(out.Credit) > 0 {
		fmt.Fprintln(w, "CREDIT CARDS")
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(tw, "ACCOUNT\tSTATEMENT BAL\tMIN PAY\tDUE\tLAST PAY\tAPR (PURCHASE)\tOVERDUE")
		fmt.Fprintln(tw, "-------\t-------------\t-------\t---\t--------\t--------------\t-------")
		for _, r := range out.Credit {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Account,
				money(r.LastStatementBalance),
				money(r.MinimumPayment),
				dash(r.NextPaymentDueDate),
				money(r.LastPaymentAmount),
				percent(r.PurchaseAPR),
				boolStr(r.IsOverdue),
			)
		}
		_ = tw.Flush()
		fmt.Fprintln(w)
	}

	if len(out.Student) > 0 {
		fmt.Fprintln(w, "STUDENT LOANS")
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(tw, "ACCOUNT\tRATE\tOUTSTANDING INT\tMIN PAY\tDUE\tPAYOFF\tSTATUS")
		fmt.Fprintln(tw, "-------\t----\t---------------\t-------\t---\t------\t------")
		for _, r := range out.Student {
			fmt.Fprintf(tw, "%s\t%.2f%%\t%s\t%s\t%s\t%s\t%s\n",
				r.Account,
				r.InterestRate,
				money(r.OutstandingInterest),
				money(r.MinimumPayment),
				dash(r.NextPaymentDueDate),
				dash(r.ExpectedPayoffDate),
				dash(r.LoanStatus),
			)
		}
		_ = tw.Flush()
		fmt.Fprintln(w)
	}

	if len(out.Mortgage) > 0 {
		fmt.Fprintln(w, "MORTGAGES")
		tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(tw, "ACCOUNT\tRATE\tNEXT PAY\tDUE\tORIG PRINCIPAL\tMATURITY\tESCROW")
		fmt.Fprintln(tw, "-------\t----\t--------\t---\t--------------\t--------\t------")
		for _, r := range out.Mortgage {
			rate := "-"
			if r.InterestRate != nil {
				rate = fmt.Sprintf("%.2f%% (%s)", *r.InterestRate, dash(r.InterestRateType))
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Account,
				rate,
				money(r.NextMonthlyPayment),
				dash(r.NextPaymentDueDate),
				money(r.OriginationPrincipal),
				dash(r.MaturityDate),
				money(r.EscrowBalance),
			)
		}
		_ = tw.Flush()
		fmt.Fprintln(w)
	}
}

func writeLiabilitiesCSV(w *os.File, out liabilitiesOutput) error {
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"Type", "Account", "Account ID", "Balance/Outstanding", "Rate/APR",
		"Min Payment", "Next Due", "Extra",
	})
	for _, r := range out.Credit {
		_ = writer.Write([]string{
			"credit", r.Account, r.AccountID,
			moneyRaw(r.LastStatementBalance), percent(r.PurchaseAPR),
			moneyRaw(r.MinimumPayment), r.NextPaymentDueDate,
			fmt.Sprintf("overdue=%s", boolStr(r.IsOverdue)),
		})
	}
	for _, r := range out.Student {
		_ = writer.Write([]string{
			"student", r.Account, r.AccountID,
			moneyRaw(r.OutstandingInterest), fmt.Sprintf("%.2f%%", r.InterestRate),
			moneyRaw(r.MinimumPayment), r.NextPaymentDueDate,
			fmt.Sprintf("status=%s;payoff=%s", r.LoanStatus, r.ExpectedPayoffDate),
		})
	}
	for _, r := range out.Mortgage {
		rate := ""
		if r.InterestRate != nil {
			rate = fmt.Sprintf("%.2f%%", *r.InterestRate)
		}
		_ = writer.Write([]string{
			"mortgage", r.Account, r.AccountID,
			moneyRaw(r.OriginationPrincipal), rate,
			moneyRaw(r.NextMonthlyPayment), r.NextPaymentDueDate,
			fmt.Sprintf("maturity=%s;escrow=%s", r.MaturityDate, moneyRaw(r.EscrowBalance)),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("csv writing error: %w", err)
	}
	return nil
}

// --- Nullable helpers ---

func lsStr(n plaid.NullableString) string {
	if n.IsSet() && n.Get() != nil {
		return *n.Get()
	}
	return ""
}

func lsFloat(n plaid.NullableFloat64) *float64 {
	if n.IsSet() && n.Get() != nil {
		v := *n.Get()
		return &v
	}
	return nil
}

func lsBool(n plaid.NullableBool) *bool {
	if n.IsSet() && n.Get() != nil {
		v := *n.Get()
		return &v
	}
	return nil
}

// --- formatting helpers ---

// money renders a nullable dollar amount as "$1,234.56" or "-" when absent.
func money(v *float64) string {
	if v == nil {
		return "-"
	}
	return "$" + fmt.Sprintf("%.2f", *v)
}

// moneyRaw renders a nullable amount as a bare number for CSV, "" when absent.
func moneyRaw(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *v)
}

func percent(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *v)
}

func boolStr(v *bool) string {
	if v == nil {
		return "-"
	}
	if *v {
		return "yes"
	}
	return "no"
}

// dash returns "-" for an empty string, else the string unchanged.
func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
