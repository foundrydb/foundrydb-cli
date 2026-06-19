package cmd

import (
	"context"
	"fmt"
	"os"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// appServiceAuthProvidersCmd groups the social-login provider subcommands under
// `fdb app-service auth providers`.
var appServiceAuthProvidersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage social-login providers for an app service",
	Long: `List, add, update, or remove social-login (OAuth2) providers for an app service's
managed authentication. Supported providers: google, github.

Client secrets are write-only: they are stored in the platform secret store and
are never returned by any command. Use providers list to inspect configured
providers without exposing credentials.`,
}

var appServiceAuthProvidersListCmd = &cobra.Command{
	Use:   "list <app-id>",
	Short: "List configured social-login providers for an app service",
	Long: `List all social-login providers configured for the app service's managed
authentication. Prints provider id, OAuth client_id, and display name.
Client secrets are never shown.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppServiceAuthProvidersList,
}

var appServiceAuthProvidersSetCmd = &cobra.Command{
	Use:   "set <app-id>",
	Short: "Add or update a social-login provider for an app service",
	Long: `Add or update a social-login provider (Google or GitHub) for an app service's
managed authentication. Requires --provider, --client-id, and --client-secret.
The client secret is stored in the platform secret store and is never returned.

When the auth configuration is Active, the issuer redeploys automatically to
pick up the new credentials.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppServiceAuthProvidersSet,
}

var appServiceAuthProvidersRemoveCmd = &cobra.Command{
	Use:   "remove <app-id>",
	Short: "Remove a social-login provider from an app service",
	Long: `Remove one social-login provider from an app service's managed authentication.
Requires --provider. The remaining configured providers are printed after removal.

When the auth configuration is Active, the issuer redeploys automatically so
the removed provider's login button disappears for end users.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppServiceAuthProvidersRemove,
}

func init() {
	// providers set flags
	appServiceAuthProvidersSetCmd.Flags().String("provider", "", "Social-login provider: google or github (required)")
	appServiceAuthProvidersSetCmd.Flags().String("client-id", "", "OAuth application client ID (required)")
	appServiceAuthProvidersSetCmd.Flags().String("client-secret", "", "OAuth application client secret -- write-only, stored in the platform secret store (required)")
	appServiceAuthProvidersSetCmd.Flags().String("display-name", "", "Button label shown on the hosted login page (defaults to provider name when omitted)")
	appServiceAuthProvidersSetCmd.MarkFlagRequired("provider")
	appServiceAuthProvidersSetCmd.MarkFlagRequired("client-id")
	appServiceAuthProvidersSetCmd.MarkFlagRequired("client-secret")

	// providers remove flags
	appServiceAuthProvidersRemoveCmd.Flags().String("provider", "", "Social-login provider to remove: google or github (required)")
	appServiceAuthProvidersRemoveCmd.MarkFlagRequired("provider")

	// Wire subcommands
	appServiceAuthProvidersCmd.AddCommand(appServiceAuthProvidersListCmd)
	appServiceAuthProvidersCmd.AddCommand(appServiceAuthProvidersSetCmd)
	appServiceAuthProvidersCmd.AddCommand(appServiceAuthProvidersRemoveCmd)

	// Attach providers group to the auth command group
	appServiceAuthCmd.AddCommand(appServiceAuthProvidersCmd)
}

func runAppServiceAuthProvidersList(cmd *cobra.Command, args []string) error {
	appID := args[0]

	client := newClient()
	ctx := context.Background()
	providers, err := client.ListAppServiceAuthProviders(ctx, appID)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(providers)
	}

	if len(providers) == 0 {
		fmt.Printf("No social-login providers configured for app service %s.\n", appID)
		fmt.Println("Use 'fdb app-service auth providers set' to add one.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"PROVIDER", "CLIENT_ID", "DISPLAY_NAME"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, p := range providers {
		displayName := p.DisplayName
		if displayName == "" {
			displayName = "-"
		}
		table.Append([]string{p.Provider, p.ClientID, displayName})
	}
	table.Render()
	fmt.Printf("Total: %s\n", pluralize(len(providers), "provider", "providers"))
	return nil
}

func runAppServiceAuthProvidersSet(cmd *cobra.Command, args []string) error {
	appID := args[0]

	provider, _ := cmd.Flags().GetString("provider")
	clientID, _ := cmd.Flags().GetString("client-id")
	clientSecret, _ := cmd.Flags().GetString("client-secret")
	displayName, _ := cmd.Flags().GetString("display-name")

	if provider != foundrydb.AuthIDPProviderGoogle && provider != foundrydb.AuthIDPProviderGitHub {
		return fmt.Errorf("invalid --provider %q: must be 'google' or 'github'", provider)
	}

	req := foundrydb.UpsertAppServiceAuthProviderRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		DisplayName:  displayName,
	}

	client := newClient()
	ctx := context.Background()
	providers, err := client.UpsertAppServiceAuthProvider(ctx, appID, provider, req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(providers)
	}

	fmt.Printf("Provider %q configured for app service %s.\n\n", provider, appID)
	printAuthProviderTable(providers)
	return nil
}

func runAppServiceAuthProvidersRemove(cmd *cobra.Command, args []string) error {
	appID := args[0]

	provider, _ := cmd.Flags().GetString("provider")

	if provider != foundrydb.AuthIDPProviderGoogle && provider != foundrydb.AuthIDPProviderGitHub {
		return fmt.Errorf("invalid --provider %q: must be 'google' or 'github'", provider)
	}

	client := newClient()
	ctx := context.Background()
	remaining, err := client.RemoveAppServiceAuthProvider(ctx, appID, provider)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(remaining)
	}

	fmt.Printf("Provider %q removed from app service %s.\n", provider, appID)
	if len(remaining) == 0 {
		fmt.Println("No social-login providers remaining. Magic-link login is still active.")
		return nil
	}
	fmt.Printf("\nRemaining providers:\n")
	printAuthProviderTable(remaining)
	return nil
}

// printAuthProviderTable renders a slice of AuthIDPProviderConfig as a table.
func printAuthProviderTable(providers []foundrydb.AuthIDPProviderConfig) {
	if len(providers) == 0 {
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"PROVIDER", "CLIENT_ID", "DISPLAY_NAME"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, p := range providers {
		displayName := p.DisplayName
		if displayName == "" {
			displayName = "-"
		}
		table.Append([]string{p.Provider, p.ClientID, displayName})
	}
	table.Render()
	fmt.Printf("Total: %s\n", pluralize(len(providers), "provider", "providers"))
}
