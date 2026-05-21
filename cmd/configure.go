package cmd

import (
	"bufio"
	"fmt"
	"os"
	"plaid-cli/pkg/config"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	clientIDFlag    string
	secretFlag      string
	environmentFlag string
	secureFlag      bool
)

func init() {
	configureCmd.Flags().StringVar(&clientIDFlag, "client-id", "", "Plaid Client ID")
	configureCmd.Flags().StringVar(&secretFlag, "secret", "", "Plaid Client Secret")
	configureCmd.Flags().StringVar(&environmentFlag, "environment", "", "Plaid Environment (sandbox/production/development)")
	configureCmd.Flags().BoolVar(&secureFlag, "secure", false, "Encrypt credentials and local cache using a master password")
	rootCmd.AddCommand(configureCmd)
}

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure Plaid API credentials",
	Long:  `Set up your Plaid Client ID, Client Secret, and target environment. Credentials will be stored securely at ~/.plaid-cli/config.json.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config

		// Try loading existing config first, to preserve access token if just updating secrets
		existing, err := config.LoadConfig()
		if err == nil && existing != nil {
			cfg = *existing
		}

		reader := bufio.NewReader(os.Stdin)

		// Client ID
		if clientIDFlag != "" {
			cfg.ClientID = clientIDFlag
		} else {
			defaultText := ""
			if cfg.ClientID != "" {
				defaultText = fmt.Sprintf(" [%s]", cfg.ClientID)
			}
			fmt.Printf("Enter Plaid Client ID%s: ", defaultText)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input != "" {
				cfg.ClientID = input
			}
		}

		// Secret
		if secretFlag != "" {
			cfg.Secret = secretFlag
		} else {
			defaultText := ""
			if cfg.Secret != "" {
				// Hide most of the secret for security
				hiddenSecret := cfg.Secret
				if len(hiddenSecret) > 8 {
					hiddenSecret = hiddenSecret[:4] + "..." + hiddenSecret[len(hiddenSecret)-4:]
				}
				defaultText = fmt.Sprintf(" [%s]", hiddenSecret)
			}
			fmt.Printf("Enter Plaid Secret%s: ", defaultText)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input != "" {
				cfg.Secret = input
			}
		}

		// Environment
		if environmentFlag != "" {
			cfg.Environment = strings.ToLower(environmentFlag)
		} else {
			defaultEnv := "sandbox"
			if cfg.Environment != "" {
				defaultEnv = cfg.Environment
			}
			fmt.Printf("Enter Plaid Environment (sandbox/production) [%s]: ", defaultEnv)
			input, _ := reader.ReadString('\n')
			input = strings.ToLower(strings.TrimSpace(input))
			if input == "" {
				cfg.Environment = defaultEnv
			} else {
				if input != "sandbox" && input != "production" && input != "development" {
					return fmt.Errorf("invalid environment '%s'. Allowed: sandbox, production", input)
				}
				cfg.Environment = input
			}
		}

		if cfg.ClientID == "" || cfg.Secret == "" {
			return fmt.Errorf("both Client ID and Secret are required to configure the Plaid CLI")
		}

		// Security Configuration
		isSecure := secureFlag
		if !cmd.Flags().Changed("secure") {
			fd := int(os.Stdin.Fd())
			if term.IsTerminal(fd) {
				defaultText := "n"
				if cfg.Secure {
					defaultText = "y"
				}
				fmt.Printf("Enable local encryption (AES-256-GCM)? (y/n) [%s]: ", defaultText)
				input, _ := reader.ReadString('\n')
				input = strings.ToLower(strings.TrimSpace(input))
				if input == "" {
					isSecure = cfg.Secure
				} else {
					isSecure = (input == "y" || input == "yes")
				}
			} else {
				isSecure = cfg.Secure
			}
		}

		if isSecure {
			cfg.Secure = true
			var password string
			if envPass := os.Getenv("PLAID_CLI_PASSWORD"); envPass != "" {
				password = envPass
			} else {
				fd := int(os.Stdin.Fd())
				if !term.IsTerminal(fd) {
					return fmt.Errorf("master password required for encryption, but stdin is not a terminal and PLAID_CLI_PASSWORD is not set")
				}

				fmt.Fprint(os.Stderr, "Set master password: ")
				p1Bytes, err := term.ReadPassword(fd)
				if err != nil {
					return fmt.Errorf("failed to read master password: %w", err)
				}
				fmt.Fprintln(os.Stderr)

				fmt.Fprint(os.Stderr, "Confirm master password: ")
				p2Bytes, err := term.ReadPassword(fd)
				if err != nil {
					return fmt.Errorf("failed to read password confirmation: %w", err)
				}
				fmt.Fprintln(os.Stderr)

				p1 := string(p1Bytes)
				p2 := string(p2Bytes)
				if p1 == "" {
					return fmt.Errorf("master password cannot be empty")
				}
				if p1 != p2 {
					return fmt.Errorf("passwords do not match")
				}
				password = p1
			}
			config.SetPassword(password)
		} else {
			cfg.Secure = false
		}

		err = cfg.SaveConfig()
		if err != nil {
			return fmt.Errorf("failed to save configuration: %w", err)
		}

		fmt.Println("Configuration successfully saved to ~/.plaid-cli/config.json")
		return nil
	},
}
