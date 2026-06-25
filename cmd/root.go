package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is the released build version. It defaults to "dev" for local builds
// and is overridden at release time via -ldflags "-X plaid-cli/cmd.version=...".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "plaid-cli",
	Short: "Plaid CLI is a command line tool to manage bank transaction data via Plaid API",
	Long: `A fast and secure Go-based CLI tool to configure Plaid API access,
authenticate via Plaid Link, retrieve accounts, and sync transactions locally.

Complete documentation is available at the repository.`,
	Version: version,
}

func init() {
	// Predefine the version flag so it carries a -v shorthand. Cobra's
	// InitDefaultVersionFlag only adds a --version flag if one is not already
	// present, so defining it here keeps cobra's built-in version handling while
	// adding the short form.
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}

// Execute runs the Cobra CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
