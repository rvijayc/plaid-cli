package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "plaid-cli",
	Short: "Plaid CLI is a command line tool to manage bank transaction data via Plaid API",
	Long: `A fast and secure Go-based CLI tool to configure Plaid API access,
authenticate via Plaid Link, retrieve accounts, and sync transactions locally.

Complete documentation is available at the repository.`,
}

// Execute runs the Cobra CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
