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

	// Liabilities is requested as "required if supported" rather than as a primary
	// product: institutions that don't offer it still link successfully, while
	// supported institutions initialize liability data for the `liabilities` command.
	// https://plaid.com/docs/api/link/#link-token-create-request-required-if-supported-products
	request.SetRequiredIfSupportedProducts([]plaid.Products{plaid.PRODUCTS_LIABILITIES})

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

// FetchLiabilities retrieves liability accounts (credit cards, student loans, and
// mortgages) for the given access token via /liabilities/get. The returned response
// also carries the AccountBase list so callers can render account labels without a
// separate /accounts/get call.
func FetchLiabilities(client *plaid.APIClient, accessToken string) (*plaid.LiabilitiesGetResponse, error) {
	ctx := context.Background()

	request := plaid.NewLiabilitiesGetRequest(accessToken)
	resp, _, err := client.PlaidApi.LiabilitiesGet(ctx).LiabilitiesGetRequest(*request).Execute()
	if err != nil {
		return nil, formatError(err)
	}

	return &resp, nil
}

// RemoveItem invalidates the access token server-side via /item/remove.
func RemoveItem(client *plaid.APIClient, accessToken string) error {
	ctx := context.Background()
	request := plaid.NewItemRemoveRequest(accessToken)
	_, _, err := client.PlaidApi.ItemRemove(ctx).ItemRemoveRequest(*request).Execute()
	if err != nil {
		return formatError(err)
	}
	return nil
}

// GetInstitutionInfo returns the institution_id and display name for the given access token.
// A missing institution on the item is not an error — it returns empty strings.
func GetInstitutionInfo(client *plaid.APIClient, accessToken string) (institutionID, institutionName string, err error) {
	ctx := context.Background()

	itemResp, _, err := client.PlaidApi.ItemGet(ctx).ItemGetRequest(*plaid.NewItemGetRequest(accessToken)).Execute()
	if err != nil {
		return "", "", formatError(err)
	}

	if !itemResp.Item.InstitutionId.IsSet() || itemResp.Item.InstitutionId.Get() == nil {
		return "", "", nil
	}
	institutionID = *itemResp.Item.InstitutionId.Get()

	instResp, _, err := client.PlaidApi.InstitutionsGetById(ctx).
		InstitutionsGetByIdRequest(*plaid.NewInstitutionsGetByIdRequest(
			institutionID,
			[]plaid.CountryCode{plaid.COUNTRYCODE_US, plaid.COUNTRYCODE_CA},
		)).Execute()
	if err != nil {
		return institutionID, "", formatError(err)
	}

	return institutionID, instResp.Institution.Name, nil
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
