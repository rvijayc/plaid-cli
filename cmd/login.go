package cmd

import (
	"fmt"
	"os/exec"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"plaid-cli/pkg/server"
	"runtime"

	"github.com/spf13/cobra"
)

var portFlag int

func init() {
	loginCmd.Flags().IntVar(&portFlag, "port", 8080, "Local port to spin up the Plaid Link flow page")
	rootCmd.AddCommand(loginCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate bank account using Plaid Link",
	Long: `Start a temporary local server and open your browser to run the Plaid Link authentication flow.
Once completed, the public token will be exchanged for an access token and stored locally.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Load configuration
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		// 2. Initialize Plaid API Client
		plaidClient, err := client.NewPlaidClient(cfg)
		if err != nil {
			return err
		}

		// 3. Create Link Token
		redirectURI := ""
		// If running in production or development, they might need an OAuth redirect URI registered with Plaid,
		// e.g. "http://localhost:8080/oauth". But let's leave it blank or default to empty as Plaid Link works
		// without it for Sandbox and most simple cases.
		fmt.Println("Generating Plaid Link token...")
		linkToken, err := client.CreateLinkToken(plaidClient, redirectURI)
		if err != nil {
			return fmt.Errorf("failed to create link token: %w", err)
		}

		// 4. Start Local Auth Server
		url := fmt.Sprintf("http://localhost:%d", portFlag)
		fmt.Printf("Starting local authentication server on %s ...\n", url)
		fmt.Println("Opening web browser to complete authentication...")

		// Run browser open in a helper
		go func() {
			_ = openBrowser(url)
		}()

		// Start server and block until token is received or it times out
		publicToken, err := server.StartServer(portFlag, linkToken)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		fmt.Println("\nAuthentication completed by browser!")
		fmt.Println("Exchanging Plaid public token for access token...")

		// 5. Exchange Public Token
		accessToken, itemID, err := client.ExchangePublicToken(plaidClient, publicToken)
		if err != nil {
			return fmt.Errorf("failed to exchange public token: %w", err)
		}

		// 6. Fetch institution metadata (best-effort; non-fatal on error)
		institutionID, institutionName, err := client.GetInstitutionInfo(plaidClient, accessToken)
		if err != nil {
			fmt.Printf("Warning: could not fetch institution name: %v\n", err)
		}

		// 7. Save Credentials
		cfg.AccessToken = accessToken
		cfg.ItemID = itemID

		found := false
		for i, item := range cfg.Items {
			if item.ItemID == itemID {
				cfg.Items[i].AccessToken = accessToken
				cfg.Items[i].InstitutionID = institutionID
				cfg.Items[i].InstitutionName = institutionName
				found = true
				break
			}
		}
		if !found {
			cfg.Items = append(cfg.Items, config.LinkedItem{
				ItemID:          itemID,
				AccessToken:     accessToken,
				InstitutionID:   institutionID,
				InstitutionName: institutionName,
			})
		}

		err = cfg.SaveConfig()
		if err != nil {
			return fmt.Errorf("failed to save access token: %w", err)
		}

		fmt.Println("\nSuccess! Access token and Item ID have been saved securely.")
		fmt.Printf("Item ID:     %s\n", itemID)
		fmt.Printf("Environment: %s\n", cfg.Environment)
		fmt.Println("You can now run 'plaid-cli accounts' or 'plaid-cli sync' to pull transactions.")
		return nil
	},
}

// openBrowser opens the specified URL in the default browser of the user.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return exec.Command(cmd, args...).Start()
}
