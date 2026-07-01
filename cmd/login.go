package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"plaid-cli/pkg/client"
	"plaid-cli/pkg/config"
	"plaid-cli/pkg/server"
	"runtime"
	"strings"

	"github.com/plaid/plaid-go/v20/plaid"
	"github.com/spf13/cobra"
)

var (
	portFlag   int
	updateFlag bool
)

func init() {
	loginCmd.Flags().IntVar(&portFlag, "port", 8080, "Local port to spin up the Plaid Link flow page")
	loginCmd.Flags().BoolVar(&updateFlag, "update", false, "Re-link an existing Item in update mode to add the Liabilities/Investments products (no new Item is created)")
	rootCmd.AddCommand(loginCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login [item_id|number]",
	Short: "Authenticate bank account using Plaid Link",
	Long: `Start a temporary local server and open your browser to run the Plaid Link authentication flow.

Without flags, this links a new bank account and stores a new access token.

With --update, it re-authenticates an existing Item in update mode to add the
Liabilities and Investments products to an Item that was linked before those
products were requested. The access token and Item ID are unchanged — no new Item
is created. Target the Item by list number or item_id, or omit to choose interactively.`,
	Args: cobra.MaximumNArgs(1),
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

		// Update mode re-links an existing Item in place rather than creating a new one.
		if updateFlag {
			return runUpdateLink(cfg, plaidClient, args)
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

// runUpdateLink re-authenticates an existing Item in update mode so the
// Liabilities/Investments products can be added to it. The access token and Item ID
// are preserved; on completion the public token is intentionally NOT exchanged.
func runUpdateLink(cfg *config.Config, plaidClient *plaid.APIClient, args []string) error {
	if len(cfg.Items) == 0 {
		return fmt.Errorf("no linked items to update. Run 'plaid-cli login' to link one first")
	}

	idx, err := resolveUpdateTarget(cfg, args)
	if err != nil {
		return err
	}
	target := &cfg.Items[idx]
	name := target.InstitutionName
	if name == "" {
		name = target.ItemID
	}

	// Determine which of the addable products this institution actually supports.
	// additional_consented_products is validated server-side, so requesting an
	// unsupported product (e.g. Investments at a mortgage servicer) hard-fails.
	products, err := supportedUpdateProducts(cfg, plaidClient, target)
	if err != nil {
		return err
	}
	if len(products) == 0 {
		fmt.Printf("%q supports neither Liabilities nor Investments via Plaid — nothing to add.\n", name)
		return nil
	}

	labels := make([]string, len(products))
	for i, p := range products {
		labels[i] = titleCase(string(p))
	}
	fmt.Printf("Re-linking %q in update mode to add: %s...\n", name, strings.Join(labels, ", "))
	fmt.Println("Generating Plaid Link update token...")
	linkToken, err := client.CreateUpdateLinkToken(plaidClient, target.AccessToken, "", products)
	if err != nil {
		return fmt.Errorf("failed to create update link token: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d", portFlag)
	fmt.Printf("Starting local authentication server on %s ...\n", url)
	fmt.Println("Opening web browser to complete re-authentication...")

	go func() {
		_ = openBrowser(url)
	}()

	// In update mode the returned public token is ignored — the access token does
	// not change. We only need to know the flow completed successfully.
	if _, err := server.StartServer(portFlag, linkToken); err != nil {
		return fmt.Errorf("re-authentication failed: %w", err)
	}

	fmt.Println("\nRe-authentication completed by browser!")

	// Persist any institution-ID backfill done during product resolution. The
	// account directory is intentionally NOT re-fetched here — re-linking doesn't
	// change account membership, and sync/liabilities/investments refresh it anyway.
	if err := cfg.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save updated config: %w", err)
	}

	fmt.Printf("\nSuccess! Item %q updated in place (Item ID unchanged: %s).\n", name, target.ItemID)
	fmt.Println("If the institution supports them, you can now run 'plaid-cli liabilities' or 'plaid-cli investments holdings'.")
	return nil
}

// supportedUpdateProducts returns the subset of addable products (Liabilities,
// Investments) that the target Item's institution supports. It backfills the
// institution ID when missing so the lookup works on older Items.
func supportedUpdateProducts(cfg *config.Config, plaidClient *plaid.APIClient, target *config.LinkedItem) ([]plaid.Products, error) {
	instID := target.InstitutionID
	if instID == "" {
		if id, instName, ierr := client.GetInstitutionInfo(plaidClient, target.AccessToken); ierr == nil {
			instID = id
			if id != "" {
				target.InstitutionID = id
			}
			if instName != "" && target.InstitutionName == "" {
				target.InstitutionName = instName
			}
		}
	}
	if instID == "" {
		return nil, fmt.Errorf("could not determine the institution for this Item; cannot decide which products to add")
	}

	supported, err := client.InstitutionSupportedProducts(plaidClient, instID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up institution products: %w", err)
	}
	supportedSet := make(map[plaid.Products]bool, len(supported))
	for _, p := range supported {
		supportedSet[p] = true
	}

	var desired []plaid.Products
	for _, p := range []plaid.Products{plaid.PRODUCTS_LIABILITIES, plaid.PRODUCTS_INVESTMENTS} {
		if supportedSet[p] {
			desired = append(desired, p)
		}
	}
	return desired, nil
}

// titleCase upper-cases the first letter of s (ASCII), leaving the rest unchanged.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// resolveUpdateTarget returns the index into cfg.Items of the Item to re-link,
// resolving a positional arg as a 1-based list number or an item_id, or prompting
// interactively when no arg is given.
func resolveUpdateTarget(cfg *config.Config, args []string) (int, error) {
	if len(args) == 1 {
		arg := args[0]

		var num int
		if _, serr := fmt.Sscanf(arg, "%d", &num); serr == nil && num >= 1 && num <= len(cfg.Items) {
			return num - 1, nil
		}
		for i := range cfg.Items {
			if cfg.Items[i].ItemID == arg {
				return i, nil
			}
		}
		return -1, fmt.Errorf("no linked item found for %q (use a list number or item_id)", arg)
	}

	fmt.Println("Linked accounts:")
	for i, item := range cfg.Items {
		name := item.InstitutionName
		if name == "" {
			name = item.ItemID
		}
		fmt.Printf("  [%d] %s\n", i+1, name)
	}
	fmt.Print("\nEnter number to re-link: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	var num int
	if _, serr := fmt.Sscanf(strings.TrimSpace(line), "%d", &num); serr != nil || num < 1 || num > len(cfg.Items) {
		return -1, fmt.Errorf("invalid selection")
	}
	return num - 1, nil
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
