package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// appServiceCmd is the top-level group for all app-service subcommands.
var appServiceCmd = &cobra.Command{
	Use:   "app-service",
	Short: "Manage app services and their features",
}

// appServiceAuthCmd groups the auth-as-a-service subcommands under
// `fdb app-service auth`.
var appServiceAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage end-user authentication for an app service",
	Long: `Enable and manage a managed OIDC identity provider for an app's end users.

Authentication is backed by one of the app's attached PostgreSQL services.
End-user identity data lives in the customer's own database under the managed
_mdb_auth schema; the platform stores enablement state only.`,
}

var appServiceAuthEnableCmd = &cobra.Command{
	Use:   "enable <app-id>",
	Short: "Enable end-user authentication for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppServiceAuthEnable,
}

var appServiceAuthGetCmd = &cobra.Command{
	Use:   "get <app-id>",
	Short: "Get the auth configuration for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppServiceAuthGet,
}

var appServiceAuthDisableCmd = &cobra.Command{
	Use:   "disable <app-id>",
	Short: "Disable end-user authentication for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppServiceAuthDisable,
}

var appServiceAuthRotateKeyCmd = &cobra.Command{
	Use:   "rotate-key <app-id>",
	Short: "Rotate the JWT signing key for an app service",
	Long: `Rotates the JWT signing key. Rotation is dual-kid: the new key is published
alongside the outgoing one so tokens signed by the previous key keep validating
until it retires.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppServiceAuthRotateKey,
}

var appServiceAuthRevokeSessionCmd = &cobra.Command{
	Use:   "revoke-session <app-id> <session-id>",
	Short: "Revoke one end-user session",
	Long: `Revokes a single end-user session. The revocation is dispatched asynchronously
to the backing database's primary VM and applied in the customer's _mdb_auth schema.`,
	Args: cobra.ExactArgs(2),
	RunE: runAppServiceAuthRevokeSession,
}

var appServiceAuthEraseUserCmd = &cobra.Command{
	Use:   "erase-user <app-id>",
	Short: "Erase one end-user (GDPR right to erasure)",
	Long: `Erases one end-user under the GDPR right to erasure (Art. 17).

The erasure removes the user and all associated identity data (identities,
sessions, refresh tokens, MFA enrolments, pending login and OAuth tokens)
and scrubs the user's audit-log rows from the customer's _mdb_auth schema.

Provide exactly one of --email or --user-id to identify the end-user.
The erasure is dispatched asynchronously to the backing database's primary VM;
the command prints the task ID for status polling.

The email address is never persisted or logged on the controller side.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppServiceAuthEraseUser,
}

func init() {
	// enable flags: required fields
	appServiceAuthEnableCmd.Flags().String("attachment-id", "", "UUID of the app's PostgreSQL attachment to back the identity store (required)")
	appServiceAuthEnableCmd.Flags().String("issuer-domain", "fallback", "Issuer domain choice: 'fallback' (auth-<id>.foundrydb.com) or 'custom'")
	appServiceAuthEnableCmd.MarkFlagRequired("attachment-id")

	// SMTP flags (required)
	appServiceAuthEnableCmd.Flags().String("smtp-host", "", "SMTP relay hostname (required)")
	appServiceAuthEnableCmd.Flags().Int("smtp-port", 587, "SMTP relay port (default 587)")
	appServiceAuthEnableCmd.Flags().String("smtp-username", "", "SMTP relay username (required)")
	appServiceAuthEnableCmd.Flags().String("smtp-password", "", "SMTP relay password or API token (required)")
	appServiceAuthEnableCmd.Flags().String("smtp-from", "", "Envelope-from address for magic-link emails (required)")
	appServiceAuthEnableCmd.Flags().String("smtp-from-name", "", "Display name shown on magic-link emails")
	appServiceAuthEnableCmd.Flags().Bool("smtp-insecure", false, "Disable STARTTLS certificate verification (for local test catchers only)")
	appServiceAuthEnableCmd.MarkFlagRequired("smtp-host")
	appServiceAuthEnableCmd.MarkFlagRequired("smtp-username")
	appServiceAuthEnableCmd.MarkFlagRequired("smtp-password")
	appServiceAuthEnableCmd.MarkFlagRequired("smtp-from")

	// Theme flags (optional)
	appServiceAuthEnableCmd.Flags().String("theme-display-name", "", "Product name shown on the hosted login pages")
	appServiceAuthEnableCmd.Flags().String("theme-brand-color", "", "Accent color for the hosted login pages (CSS color, e.g. '#4F46E5')")
	appServiceAuthEnableCmd.Flags().String("theme-logo-url", "", "URL of the logo shown on the hosted login pages")
	appServiceAuthEnableCmd.Flags().String("theme-support-url", "", "URL of a support page linked from the login pages")

	// Social login (IDP) flags: repeatable flag accepts "provider:client_id:client_secret[:display_name]"
	appServiceAuthEnableCmd.Flags().StringArrayP("idp-provider", "p", nil,
		"Social login provider in the form 'provider:client_id:client_secret[:display_name]'.\n"+
			"Supported providers: google, github.\n"+
			"May be specified multiple times. Example:\n"+
			"  --idp-provider google:my-client-id:my-secret:'Sign in with Google'")

	// erase-user flags: exactly one of --email or --user-id is required
	appServiceAuthEraseUserCmd.Flags().String("email", "", "Email address of the end-user to erase (mutually exclusive with --user-id)")
	appServiceAuthEraseUserCmd.Flags().String("user-id", "", "Auth subject UUID of the end-user to erase (mutually exclusive with --email)")

	// Wire the subcommands
	appServiceAuthCmd.AddCommand(appServiceAuthEnableCmd)
	appServiceAuthCmd.AddCommand(appServiceAuthGetCmd)
	appServiceAuthCmd.AddCommand(appServiceAuthDisableCmd)
	appServiceAuthCmd.AddCommand(appServiceAuthRotateKeyCmd)
	appServiceAuthCmd.AddCommand(appServiceAuthRevokeSessionCmd)
	appServiceAuthCmd.AddCommand(appServiceAuthEraseUserCmd)

	appServiceCmd.AddCommand(appServiceAuthCmd)
}

// parseIDPProviders parses one or more "--idp-provider" flag values of the form
// "provider:client_id:client_secret" or
// "provider:client_id:client_secret:display_name".
func parseIDPProviders(raw []string) ([]foundrydb.AuthIDPProviderRequest, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	providers := make([]foundrydb.AuthIDPProviderRequest, 0, len(raw))
	for _, entry := range raw {
		parts := strings.SplitN(entry, ":", 4)
		if len(parts) < 3 {
			return nil, fmt.Errorf(
				"invalid --idp-provider %q: expected 'provider:client_id:client_secret[:display_name]'", entry)
		}
		p := foundrydb.AuthIDPProviderRequest{
			Provider:     parts[0],
			ClientID:     parts[1],
			ClientSecret: parts[2],
		}
		if len(parts) == 4 {
			p.DisplayName = parts[3]
		}
		if p.Provider != foundrydb.AuthIDPProviderGoogle && p.Provider != foundrydb.AuthIDPProviderGitHub {
			return nil, fmt.Errorf(
				"invalid provider %q in --idp-provider %q: must be 'google' or 'github'", p.Provider, entry)
		}
		if p.ClientID == "" {
			return nil, fmt.Errorf("missing client_id in --idp-provider %q", entry)
		}
		if p.ClientSecret == "" {
			return nil, fmt.Errorf("missing client_secret in --idp-provider %q", entry)
		}
		providers = append(providers, p)
	}
	return providers, nil
}

func runAppServiceAuthEnable(cmd *cobra.Command, args []string) error {
	appID := args[0]

	attachmentID, _ := cmd.Flags().GetString("attachment-id")
	issuerDomain, _ := cmd.Flags().GetString("issuer-domain")

	// SMTP
	smtpHost, _ := cmd.Flags().GetString("smtp-host")
	smtpPort, _ := cmd.Flags().GetInt("smtp-port")
	smtpUser, _ := cmd.Flags().GetString("smtp-username")
	smtpPass, _ := cmd.Flags().GetString("smtp-password")
	smtpFrom, _ := cmd.Flags().GetString("smtp-from")
	smtpFromName, _ := cmd.Flags().GetString("smtp-from-name")
	smtpInsecure, _ := cmd.Flags().GetBool("smtp-insecure")

	// Theme (all optional)
	themeDisplayName, _ := cmd.Flags().GetString("theme-display-name")
	themeBrandColor, _ := cmd.Flags().GetString("theme-brand-color")
	themeLogoURL, _ := cmd.Flags().GetString("theme-logo-url")
	themeSupportURL, _ := cmd.Flags().GetString("theme-support-url")

	// Social login providers
	rawIDPs, _ := cmd.Flags().GetStringArray("idp-provider")
	idpProviders, err := parseIDPProviders(rawIDPs)
	if err != nil {
		return err
	}

	// Validate issuer domain choice
	if issuerDomain != foundrydb.AuthIssuerDomainFallback && issuerDomain != foundrydb.AuthIssuerDomainCustom {
		return fmt.Errorf("invalid --issuer-domain %q: must be 'fallback' or 'custom'", issuerDomain)
	}

	req := foundrydb.AuthEnableRequest{
		AttachmentID:       attachmentID,
		IssuerDomainChoice: issuerDomain,
		SMTP: foundrydb.AuthSMTPConfig{
			Host:               smtpHost,
			Port:               smtpPort,
			Username:           smtpUser,
			Password:           smtpPass,
			FromAddress:        smtpFrom,
			FromName:           smtpFromName,
			InsecureSkipVerify: smtpInsecure,
		},
		Theme: foundrydb.AuthThemeConfig{
			DisplayName: themeDisplayName,
			BrandColor:  themeBrandColor,
			LogoURL:     themeLogoURL,
			SupportURL:  themeSupportURL,
		},
		IDPProviders: idpProviders,
	}

	client := newClient()
	ctx := context.Background()
	authCfg, err := client.EnableAppServiceAuth(ctx, appID, req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(authCfg)
	}

	fmt.Printf("Auth enabled for app service %s\n\n", appID)
	printAuthConfiguration(authCfg)
	return nil
}

func runAppServiceAuthGet(cmd *cobra.Command, args []string) error {
	appID := args[0]

	client := newClient()
	ctx := context.Background()
	result, err := client.GetAppServiceAuth(ctx, appID)
	if err != nil {
		return err
	}
	if result == nil {
		fmt.Fprintf(os.Stderr, "Auth is not enabled for app service %s\n", appID)
		return nil
	}

	if jsonOut {
		return printJSON(result)
	}

	printAuthConfiguration(result.Auth)
	printSigningKeys(result.SigningKeys)
	return nil
}

func runAppServiceAuthDisable(cmd *cobra.Command, args []string) error {
	appID := args[0]

	fmt.Printf("This will disable end-user authentication for app service %s.\n", appID)
	fmt.Print("The _mdb_auth schema in the attached database is preserved. Continue? [y/N]: ")
	var input string
	fmt.Scanln(&input)
	if strings.ToLower(strings.TrimSpace(input)) != "y" {
		fmt.Println("Aborted.")
		return nil
	}

	client := newClient()
	ctx := context.Background()
	if err := client.DisableAppServiceAuth(ctx, appID); err != nil {
		return err
	}

	fmt.Printf("Auth disabled for app service %s.\n", appID)
	fmt.Println("The _mdb_auth schema in the backing database has been preserved.")
	return nil
}

func runAppServiceAuthRotateKey(cmd *cobra.Command, args []string) error {
	appID := args[0]

	client := newClient()
	ctx := context.Background()
	key, err := client.RotateAppServiceAuthKey(ctx, appID)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(key)
	}

	fmt.Printf("JWT signing key rotated for app service %s\n\n", appID)
	fmt.Printf("New key ID (kid):  %s\n", key.Kid)
	fmt.Printf("Algorithm:         %s\n", key.Algorithm)
	fmt.Printf("Status:            %s\n", key.Status)
	fmt.Printf("Key record ID:     %s\n", key.ID)
	fmt.Printf("\nThe previous key remains active until it retires (dual-kid rotation).\n")
	return nil
}

func runAppServiceAuthRevokeSession(cmd *cobra.Command, args []string) error {
	appID := args[0]
	sessionID := args[1]

	client := newClient()
	ctx := context.Background()
	if err := client.RevokeAppServiceAuthSession(ctx, appID, sessionID); err != nil {
		return err
	}

	fmt.Printf("Session %s revoked for app service %s (revocation dispatched).\n", sessionID, appID)
	return nil
}

func runAppServiceAuthEraseUser(cmd *cobra.Command, args []string) error {
	appID := args[0]

	email, _ := cmd.Flags().GetString("email")
	userID, _ := cmd.Flags().GetString("user-id")

	if email == "" && userID == "" {
		return fmt.Errorf("exactly one of --email or --user-id is required")
	}
	if email != "" && userID != "" {
		return fmt.Errorf("--email and --user-id are mutually exclusive: provide exactly one")
	}

	req := foundrydb.DeleteAppServiceAuthUserRequest{
		Email:  email,
		UserID: userID,
	}

	client := newClient()
	ctx := context.Background()
	taskID, err := client.DeleteAppServiceAuthUser(ctx, appID, req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(map[string]string{"task_id": taskID})
	}

	fmt.Printf("Erasure dispatched for app service %s\n", appID)
	fmt.Printf("Task ID: %s\n", taskID)
	fmt.Printf("\nThe erasure runs asynchronously on the backing database VM.\n")
	return nil
}

// printAuthConfiguration renders an AuthConfiguration in human-readable form.
func printAuthConfiguration(cfg *foundrydb.AuthConfiguration) {
	if cfg == nil {
		return
	}
	fmt.Printf("ID:              %s\n", cfg.ID)
	fmt.Printf("App service:     %s\n", cfg.AppServiceID)
	fmt.Printf("Attachment:      %s\n", cfg.AttachmentID)
	fmt.Printf("Status:          %s\n", cfg.Status)
	fmt.Printf("Issuer URL:      %s\n", cfg.IssuerURL)
	fmt.Printf("Fallback domain: %s\n", cfg.FallbackDomain)
	if cfg.CustomDomain != "" {
		fmt.Printf("Custom domain:   %s\n", cfg.CustomDomain)
	}
	if cfg.SchemaVersionApplied != "" {
		fmt.Printf("Schema version:  %s\n", cfg.SchemaVersionApplied)
	}
	if cfg.FailureReason != "" {
		fmt.Printf("Failure reason:  %s\n", cfg.FailureReason)
	}

	if cfg.Theme.DisplayName != "" || cfg.Theme.BrandColor != "" || cfg.Theme.LogoURL != "" {
		fmt.Printf("\nTheme:\n")
		if cfg.Theme.DisplayName != "" {
			fmt.Printf("  Display name:  %s\n", cfg.Theme.DisplayName)
		}
		if cfg.Theme.BrandColor != "" {
			fmt.Printf("  Brand color:   %s\n", cfg.Theme.BrandColor)
		}
		if cfg.Theme.LogoURL != "" {
			fmt.Printf("  Logo URL:      %s\n", cfg.Theme.LogoURL)
		}
		if cfg.Theme.SupportURL != "" {
			fmt.Printf("  Support URL:   %s\n", cfg.Theme.SupportURL)
		}
	}

	if len(cfg.IDPProviders) > 0 {
		fmt.Printf("\nSocial login providers:\n")
		for _, p := range cfg.IDPProviders {
			name := p.DisplayName
			if name == "" {
				name = p.Provider
			}
			fmt.Printf("  %s (%s) client_id=%s\n", name, p.Provider, p.ClientID)
		}
	} else {
		fmt.Printf("\nSocial login: magic-link only (no providers configured)\n")
	}
}

// printSigningKeys renders the list of JWT signing key records in a table.
func printSigningKeys(keys []foundrydb.AuthSigningKey) {
	if len(keys) == 0 {
		return
	}

	fmt.Printf("\nSigning keys:\n")
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"KID", "ALGORITHM", "STATUS", "ACTIVATED"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, k := range keys {
		activated := ""
		if k.ActivatedAt != nil {
			activated = k.ActivatedAt.Format("2006-01-02 15:04 UTC")
		}
		table.Append([]string{
			truncate(k.Kid, 16),
			k.Algorithm,
			k.Status,
			activated,
		})
	}
	table.Render()
	fmt.Printf("Total: %s\n", pluralize(len(keys), "key", "keys"))
}

// truncate shortens s to at most maxLen runes, appending "..." when truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// pluralize returns "N word" or "N words" depending on count.
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + singular
	}
	return strconv.Itoa(n) + " " + plural
}
