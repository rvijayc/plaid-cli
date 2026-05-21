package client

import (
	"context"
	"fmt"
	"plaid-cli/pkg/config"

	"github.com/plaid/plaid-go/v20/plaid"
)

// NewPlaidClient initializes a new Plaid client using the provided configuration.
func NewPlaidClient(cfg *config.Config) (*plaid.APIClient, error) {
	if cfg.ClientID == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("client ID and secret must not be empty")
	}

	configuration := plaid.NewConfiguration()
	configuration.AddDefaultHeader("PLAID-CLIENT-ID", cfg.ClientID)
	configuration.AddDefaultHeader("PLAID-SECRET", cfg.Secret)

	switch cfg.Environment {
	case "sandbox":
		configuration.UseEnvironment(plaid.Sandbox)
	case "production":
		configuration.UseEnvironment(plaid.Production)
	case "development":
		configuration.UseEnvironment(plaid.Development)
	default:
		return nil, fmt.Errorf("unsupported Plaid environment: %s", cfg.Environment)
	}

	return plaid.NewAPIClient(configuration), nil
}

// CreateLinkToken generates a Link token for the authentication flow.
func CreateLinkToken(client *plaid.APIClient, redirectURI string) (string, error) {
	ctx := context.Background()

	// Unique client user ID
	user := plaid.LinkTokenCreateRequestUser{
		ClientUserId: "plaid-cli-user-id",
	}

	// Build the link token create request
	request := plaid.NewLinkTokenCreateRequest(
		"Plaid CLI Tool",
		"en",
		[]plaid.CountryCode{plaid.COUNTRYCODE_US, plaid.COUNTRYCODE_CA},
		user,
	)

	// We request 'transactions' product
	request.SetProducts([]plaid.Products{plaid.PRODUCTS_TRANSACTIONS})

	// Request up to 730 days (2 years) of historical transaction data
	transactionsConfig := plaid.NewLinkTokenTransactions()
	transactionsConfig.SetDaysRequested(730)
	request.SetTransactions(*transactionsConfig)

	// Redirect URI is optional but helpful if configured
	if redirectURI != "" {
		request.SetRedirectUri(redirectURI)
	}

	resp, _, err := client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()
	if err != nil {
		return "", formatError(err)
	}

	return resp.GetLinkToken(), nil
}

// ExchangePublicToken exchanges the public token from Plaid Link for a permanent access token and item ID.
func ExchangePublicToken(client *plaid.APIClient, publicToken string) (string, string, error) {
	ctx := context.Background()

	request := plaid.NewItemPublicTokenExchangeRequest(publicToken)
	resp, _, err := client.PlaidApi.ItemPublicTokenExchange(ctx).ItemPublicTokenExchangeRequest(*request).Execute()
	if err != nil {
		return "", "", formatError(err)
	}

	return resp.GetAccessToken(), resp.GetItemId(), nil
}

// FetchAccounts retrieves the list of accounts associated with the stored AccessToken.
func FetchAccounts(client *plaid.APIClient, accessToken string) ([]plaid.AccountBase, error) {
	ctx := context.Background()

	request := plaid.NewAccountsGetRequest(accessToken)
	resp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*request).Execute()
	if err != nil {
		return nil, formatError(err)
	}

	return resp.GetAccounts(), nil
}

// SyncTransactionsPage fetches a single page of transaction changes starting from the cursor.
func SyncTransactionsPage(client *plaid.APIClient, accessToken string, cursor string) (string, []plaid.Transaction, []plaid.Transaction, []plaid.RemovedTransaction, bool, error) {
	ctx := context.Background()

	request := plaid.NewTransactionsSyncRequest(accessToken)
	if cursor != "" {
		request.SetCursor(cursor)
	}

	resp, _, err := client.PlaidApi.TransactionsSync(ctx).TransactionsSyncRequest(*request).Execute()
	if err != nil {
		return "", nil, nil, nil, false, formatError(err)
	}

	return resp.GetNextCursor(), resp.GetAdded(), resp.GetModified(), resp.GetRemoved(), resp.GetHasMore(), nil
}

// formatError converts generic Plaid API client errors into structured plaid.PlaidError descriptions.
func formatError(err error) error {
	if err == nil {
		return nil
	}
	if plaidErr, convertErr := plaid.ToPlaidError(err); convertErr == nil {
		displayMsg := "none"
		if plaidErr.DisplayMessage.IsSet() && plaidErr.DisplayMessage.Get() != nil {
			displayMsg = *plaidErr.DisplayMessage.Get()
		}
		return fmt.Errorf("Plaid API Error [%s]: %s (Type: %s, Display: %s)",
			plaidErr.ErrorCode, plaidErr.ErrorMessage, plaidErr.ErrorType, displayMsg)
	}
	return err
}
