package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"plaid-cli/pkg/config"
	"plaid-cli/pkg/rules"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	rulesListFormatFlag string

	ruleNameFlag        string
	ruleMatchFlag       string
	ruleRegexFlag       string
	ruleAccountIDFlag   string
	ruleMinAmountFlag   float64
	ruleMaxAmountFlag   float64
	ruleCategoryIsFlag  string
	ruleSetCategoryFlag string
	ruleRenameFlag      string
	ruleTagFlag         []string
	ruleIgnoreFlag      bool

	rulesApplyDryRunFlag bool

	rulesTestMatchFlag     string
	rulesTestRegexFlag     string
	rulesTestMinAmountFlag float64
	rulesTestMaxAmountFlag float64
)

func init() {
	rulesListCmd.Flags().StringVar(&rulesListFormatFlag, "format", "table", "Output format (table/json)")

	rulesAddCmd.Flags().StringVar(&ruleNameFlag, "name", "", "Human-readable rule name")
	rulesAddCmd.Flags().StringVar(&ruleMatchFlag, "match", "", "Case-insensitive substring match on transaction name")
	rulesAddCmd.Flags().StringVar(&ruleRegexFlag, "regex", "", "Go regular expression match on transaction name")
	rulesAddCmd.Flags().StringVar(&ruleAccountIDFlag, "account-id", "", "Exact match on Plaid account ID")
	rulesAddCmd.Flags().Float64Var(&ruleMinAmountFlag, "min-amount", 0.0, "Inclusive lower bound on transaction amount")
	rulesAddCmd.Flags().Float64Var(&ruleMaxAmountFlag, "max-amount", 0.0, "Inclusive upper bound on transaction amount")
	rulesAddCmd.Flags().StringVar(&ruleCategoryIsFlag, "category-is", "", "Case-insensitive substring match on Plaid's category string")
	rulesAddCmd.Flags().StringVar(&ruleSetCategoryFlag, "set-category", "", "User-defined category to assign")
	rulesAddCmd.Flags().StringVar(&ruleRenameFlag, "rename", "", "Display name override")
	rulesAddCmd.Flags().StringArrayVar(&ruleTagFlag, "tag", nil, "Tag to attach (repeatable)")
	rulesAddCmd.Flags().BoolVar(&ruleIgnoreFlag, "ignore", false, "Hide matching transactions from budget/spend summaries")

	rulesApplyCmd.Flags().BoolVar(&rulesApplyDryRunFlag, "dry-run", false, "Print matches without writing overrides")

	rulesTestCmd.Flags().StringVar(&rulesTestMatchFlag, "match", "", "Case-insensitive substring match on transaction name")
	rulesTestCmd.Flags().StringVar(&rulesTestRegexFlag, "regex", "", "Go regular expression match on transaction name")
	rulesTestCmd.Flags().Float64Var(&rulesTestMinAmountFlag, "min-amount", 0.0, "Inclusive lower bound on transaction amount")
	rulesTestCmd.Flags().Float64Var(&rulesTestMaxAmountFlag, "max-amount", 0.0, "Inclusive upper bound on transaction amount")

	rulesCmd.AddCommand(rulesListCmd)
	rulesCmd.AddCommand(rulesAddCmd)
	rulesCmd.AddCommand(rulesRemoveCmd)
	rulesCmd.AddCommand(rulesEnableCmd)
	rulesCmd.AddCommand(rulesDisableCmd)
	rulesCmd.AddCommand(rulesApplyCmd)
	rulesCmd.AddCommand(rulesTestCmd)

	rootCmd.AddCommand(rulesCmd)
}

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Manage custom auto-categorization rules",
	Long:  `Create and manage non-destructive rules that override transaction names, categories, and tags at render time.`,
}

var rulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		rf, err := rules.LoadRules()
		if err != nil {
			return err
		}

		switch strings.ToLower(rulesListFormatFlag) {
		case "json":
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(rf.Rules); err != nil {
				return fmt.Errorf("failed to encode rules to JSON: %w", err)
			}
		case "table":
			if len(rf.Rules) == 0 {
				fmt.Fprintln(os.Stderr, "No rules defined. Add one with 'plaid-cli rules add'.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
			fmt.Fprintln(w, "ID\tNAME\tENABLED\tCONDITIONS\tACTIONS")
			fmt.Fprintln(w, "--\t----\t-------\t----------\t-------")
			for _, r := range rf.Rules {
				fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\n",
					r.ID, r.Name, r.Enabled, conditionsSummary(r.Conditions), actionsSummary(r.Actions))
			}
			_ = w.Flush()
		default:
			return fmt.Errorf("unsupported output format '%s'. Choose table or json", rulesListFormatFlag)
		}
		return nil
	},
}

var rulesAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new rule",
	RunE: func(cmd *cobra.Command, args []string) error {
		rf, err := rules.LoadRules()
		if err != nil {
			return err
		}

		reader := bufio.NewReader(os.Stdin)

		name := ruleNameFlag
		if name == "" {
			name = promptIfTerminal(reader, "Rule name: ")
		}

		conds := rules.Conditions{
			NameContains: ruleMatchFlag,
			NameRegex:    ruleRegexFlag,
			AccountID:    ruleAccountIDFlag,
			AmountMin:    ruleMinAmountFlag,
			AmountMax:    ruleMaxAmountFlag,
			CategoryIs:   ruleCategoryIsFlag,
		}

		// Interactively prompt for core matching fields if none supplied in a terminal.
		if isTerminal() && conds.NameContains == "" && conds.NameRegex == "" &&
			conds.AccountID == "" && conds.CategoryIs == "" &&
			!cmd.Flags().Changed("min-amount") && !cmd.Flags().Changed("max-amount") {
			conds.NameContains = promptIfTerminal(reader, "Match name contains (substring): ")
			if v := promptIfTerminal(reader, "Min amount (blank to skip): "); v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					conds.AmountMin = f
				}
			}
			if v := promptIfTerminal(reader, "Max amount (blank to skip): "); v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					conds.AmountMax = f
				}
			}
		}

		actions := rules.Actions{
			Rename:      ruleRenameFlag,
			SetCategory: ruleSetCategoryFlag,
			Tags:        ruleTagFlag,
			Ignore:      ruleIgnoreFlag,
		}

		if isTerminal() && actions.Rename == "" && actions.SetCategory == "" &&
			len(actions.Tags) == 0 && !cmd.Flags().Changed("ignore") {
			actions.Rename = promptIfTerminal(reader, "Rename to (blank to skip): ")
			actions.SetCategory = promptIfTerminal(reader, "Set category (blank to skip): ")
			if tags := promptIfTerminal(reader, "Tags (comma-separated, blank to skip): "); tags != "" {
				for _, t := range strings.Split(tags, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						actions.Tags = append(actions.Tags, t)
					}
				}
			}
		}

		if conds == (rules.Conditions{}) {
			return fmt.Errorf("at least one condition is required to add a rule")
		}
		if actions.Rename == "" && actions.SetCategory == "" && len(actions.Tags) == 0 && !actions.Ignore {
			return fmt.Errorf("at least one action is required to add a rule")
		}

		rule := rules.Rule{
			ID:         rules.NewRuleID(),
			Name:       name,
			Enabled:    true,
			Conditions: conds,
			Actions:    actions,
		}
		rf.Rules = append(rf.Rules, rule)

		if err := rf.SaveRules(); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Added rule %s (%s)\n", rule.ID, rule.Name)
		return nil
	},
}

var rulesRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete a rule by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateRule(args[0], func(r *rules.Rule) {}, true)
	},
}

var rulesEnableCmd = &cobra.Command{
	Use:   "enable <id>",
	Short: "Enable a disabled rule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateRule(args[0], func(r *rules.Rule) { r.Enabled = true }, false)
	},
}

var rulesDisableCmd = &cobra.Command{
	Use:   "disable <id>",
	Short: "Disable a rule without deleting it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateRule(args[0], func(r *rules.Rule) { r.Enabled = false }, false)
	},
}

var rulesApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Re-run all enabled rules against the full transaction cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		rf, err := rules.LoadRules()
		if err != nil {
			return err
		}
		cache, err := config.LoadCache()
		if err != nil {
			return err
		}

		if rulesApplyDryRunFlag {
			matches := 0
			for _, tx := range cache.Transactions {
				for i := range rf.Rules {
					r := &rf.Rules[i]
					if !r.Enabled {
						continue
					}
					if r.Match(tx) {
						fmt.Printf("%s  %-30s  %.2f  => rule %s (%s)\n",
							tx.Date, truncate(tx.Name, 30), tx.Amount, r.ID, r.Name)
						matches++
						break
					}
				}
			}
			fmt.Fprintf(os.Stderr, "Dry run: %d transactions would be overridden (no changes written).\n", matches)
			return nil
		}

		count := rules.ApplyAll(cache, rf, nil)
		if err := cache.SaveCache(); err != nil {
			return fmt.Errorf("failed to save cache: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Applied rules: %d overrides written.\n", count)
		return nil
	},
}

var rulesTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Dry-run a condition against the cache and print matching transactions",
	RunE: func(cmd *cobra.Command, args []string) error {
		cache, err := config.LoadCache()
		if err != nil {
			return err
		}

		probe := rules.Rule{
			Enabled: true,
			Conditions: rules.Conditions{
				NameContains: rulesTestMatchFlag,
				NameRegex:    rulesTestRegexFlag,
				AmountMin:    rulesTestMinAmountFlag,
				AmountMax:    rulesTestMaxAmountFlag,
			},
		}

		if probe.Conditions == (rules.Conditions{}) {
			return fmt.Errorf("provide at least one of --match, --regex, --min-amount, --max-amount")
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)
		fmt.Fprintln(w, "DATE\tNAME\tAMOUNT\tCATEGORY")
		fmt.Fprintln(w, "----\t----\t------\t--------")
		matches := 0
		for _, tx := range cache.Transactions {
			if probe.Match(tx) {
				fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\n", tx.Date, tx.Name, tx.Amount, rules.CategoryString(tx))
				matches++
			}
		}
		_ = w.Flush()
		fmt.Fprintf(os.Stderr, "%d matching transactions.\n", matches)
		return nil
	},
}

// mutateRule applies fn to the rule with the given ID, or removes it if remove is true.
func mutateRule(id string, fn func(*rules.Rule), remove bool) error {
	rf, err := rules.LoadRules()
	if err != nil {
		return err
	}

	idx := -1
	for i := range rf.Rules {
		if rf.Rules[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("rule %s not found", id)
	}

	if remove {
		rf.Rules = append(rf.Rules[:idx], rf.Rules[idx+1:]...)
	} else {
		fn(&rf.Rules[idx])
	}

	if err := rf.SaveRules(); err != nil {
		return err
	}

	if remove {
		fmt.Fprintf(os.Stderr, "Removed rule %s\n", id)
	} else {
		fmt.Fprintf(os.Stderr, "Updated rule %s (enabled=%t)\n", id, rf.Rules[idx].Enabled)
	}
	return nil
}

func promptIfTerminal(reader *bufio.Reader, prompt string) string {
	if !isTerminal() {
		return ""
	}
	fmt.Fprint(os.Stderr, prompt)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(input)
}

func conditionsSummary(c rules.Conditions) string {
	var parts []string
	if c.NameContains != "" {
		parts = append(parts, "name~"+c.NameContains)
	}
	if c.NameRegex != "" {
		parts = append(parts, "regex:"+c.NameRegex)
	}
	if c.AccountID != "" {
		parts = append(parts, "acct="+c.AccountID)
	}
	if c.AmountMin != 0 {
		parts = append(parts, fmt.Sprintf(">=%.2f", c.AmountMin))
	}
	if c.AmountMax != 0 {
		parts = append(parts, fmt.Sprintf("<=%.2f", c.AmountMax))
	}
	if c.CategoryIs != "" {
		parts = append(parts, "cat~"+c.CategoryIs)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " AND ")
}

func actionsSummary(a rules.Actions) string {
	var parts []string
	if a.Rename != "" {
		parts = append(parts, "rename="+a.Rename)
	}
	if a.SetCategory != "" {
		parts = append(parts, "cat="+a.SetCategory)
	}
	if len(a.Tags) > 0 {
		parts = append(parts, "tags="+strings.Join(a.Tags, ","))
	}
	if a.Ignore {
		parts = append(parts, "ignore")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
