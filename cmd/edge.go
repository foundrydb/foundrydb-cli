package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ---------------------------------------------------------------------------
// Local types mirroring the SDK edge types (not yet in the vendored SDK).
// ---------------------------------------------------------------------------

type edgeDomain struct {
	ID                    string  `json:"id"`
	ServiceID             string  `json:"service_id"`
	Domain                string  `json:"domain"`
	Status                string  `json:"status"`
	CertificateID         *string `json:"certificate_id,omitempty"`
	VerificationCheckedAt *string `json:"verification_checked_at,omitempty"`
	ErrorMessage          *string `json:"error_message,omitempty"`
	CNAMETarget           string  `json:"cname_target,omitempty"`
	CreatedAt             string  `json:"created_at"`
	UpdatedAt             string  `json:"updated_at"`
}

type edgeCacheRule struct {
	PathPrefix string `json:"path_prefix"`
	TTLSeconds int    `json:"ttl_seconds"`
}

type edgeRateLimit struct {
	RequestsPerSecond int    `json:"requests_per_second"`
	Burst             int    `json:"burst"`
	Key               string `json:"key"`
}

type edgeStatus struct {
	EdgeEnabled   bool   `json:"edge_enabled"`
	HomePoP       string `json:"home_pop,omitempty"`
	CNAMETarget   string `json:"cname_target,omitempty"`
	ConfigVersion int64  `json:"config_version"`
	Applications  []struct {
		Zone           string `json:"zone"`
		AppliedVersion int64  `json:"applied_version"`
		Status         string `json:"status"`
		ErrorMessage   string `json:"error_message,omitempty"`
	} `json:"applications,omitempty"`
}

type edgeSettings struct {
	CacheRules    []edgeCacheRule `json:"cache_rules,omitempty"`
	RateLimit     *edgeRateLimit  `json:"rate_limit,omitempty"`
	WAFMode       string          `json:"waf_mode"`
	ConfigVersion int64           `json:"config_version"`
}

// ---------------------------------------------------------------------------
// Command tree
// ---------------------------------------------------------------------------

var appsDomainsCmd = &cobra.Command{
	Use:   "domains",
	Short: "Manage custom domains for an app service",
}

var appsDomainsListCmd = &cobra.Command{
	Use:   "list <app-id>",
	Short: "List custom domains attached to an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsDomainsList,
}

var appsDomainsAddCmd = &cobra.Command{
	Use:   "add <app-id>",
	Short: "Add a custom domain to an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsDomainsAdd,
}

var appsDomainsVerifyCmd = &cobra.Command{
	Use:   "verify <app-id> <domain-id>",
	Short: "Trigger an immediate verification pass for a pending domain",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppsDomainsVerify,
}

var appsDomainsRemoveCmd = &cobra.Command{
	Use:   "remove <app-id> <domain-id>",
	Short: "Remove a custom domain from an app service",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppsDomainsRemove,
}

var appsEdgeCmd = &cobra.Command{
	Use:   "edge",
	Short: "Inspect and configure the edge tier for an app service",
}

var appsEdgeStatusCmd = &cobra.Command{
	Use:   "status <app-id>",
	Short: "Show edge status for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeStatus,
}

var appsEdgeUpdateCmd = &cobra.Command{
	Use:   "update-settings <app-id>",
	Short: "Update edge settings (cache rules, rate limit, WAF mode) for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeUpdate,
}

func init() {
	// domains add flags
	appsDomainsAddCmd.Flags().String("domain", "", "Custom domain name to add (required, e.g. app.example.com)")

	// edge update-settings flags
	appsEdgeUpdateCmd.Flags().String("cache-path-prefix", "", "Cache rule: path prefix to cache (e.g. /static)")
	appsEdgeUpdateCmd.Flags().Int("cache-ttl", 0, "Cache rule: TTL in seconds (required when --cache-path-prefix is set)")
	appsEdgeUpdateCmd.Flags().Int("rate-limit-rps", 0, "Rate limit: requests per second (omit to leave unchanged)")
	appsEdgeUpdateCmd.Flags().Int("rate-limit-burst", 0, "Rate limit: burst allowance (required when --rate-limit-rps is set)")
	appsEdgeUpdateCmd.Flags().String("rate-limit-key", "ip", "Rate limit: bucket key (ip or api_key)")
	appsEdgeUpdateCmd.Flags().String("waf-mode", "", "WAF mode: off or detect (omit to leave unchanged)")

	appsDomainsCmd.AddCommand(appsDomainsListCmd)
	appsDomainsCmd.AddCommand(appsDomainsAddCmd)
	appsDomainsCmd.AddCommand(appsDomainsVerifyCmd)
	appsDomainsCmd.AddCommand(appsDomainsRemoveCmd)

	appsEdgeCmd.AddCommand(appsEdgeStatusCmd)
	appsEdgeCmd.AddCommand(appsEdgeUpdateCmd)

	appsCmd.AddCommand(appsDomainsCmd)
	appsCmd.AddCommand(appsEdgeCmd)
}

// ---------------------------------------------------------------------------
// Command implementations
// ---------------------------------------------------------------------------

func runAppsDomainsList(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var resp struct {
		Domains []edgeDomain `json:"domains"`
	}
	if err := edgeGet(fmt.Sprintf("/app-services/%s/domains", appID), &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	if len(resp.Domains) == 0 {
		fmt.Println("No custom domains found.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "DOMAIN", "STATUS", "CNAME TARGET", "CREATED"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, d := range resp.Domains {
		shortID := d.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		created := d.CreatedAt
		if len(created) > 10 {
			created = created[:10]
		}
		table.Append([]string{shortID, d.Domain, d.Status, d.CNAMETarget, created})
	}
	table.Render()
	fmt.Printf("\nTotal: %d domain(s)\n", len(resp.Domains))
	return nil
}

func runAppsDomainsAdd(cmd *cobra.Command, args []string) error {
	domain, _ := cmd.Flags().GetString("domain")
	if domain == "" {
		fmt.Print("Custom domain (e.g. app.example.com): ")
		fmt.Scanln(&domain)
	}
	if domain == "" {
		return fmt.Errorf("--domain is required")
	}

	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	body := map[string]string{"domain": domain}
	var d edgeDomain
	if err := edgePost(fmt.Sprintf("/app-services/%s/domains", appID), body, &d); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(d)
	}

	fmt.Printf("Domain %q added (ID: %s, status: %s).\n", d.Domain, d.ID, d.Status)
	if d.CNAMETarget != "" {
		fmt.Printf("Point your DNS CNAME record for %q at: %s\n", d.Domain, d.CNAMETarget)
	}
	fmt.Printf("Use 'fdb apps domains verify %s %s' to trigger an immediate verification pass.\n", args[0], d.ID)
	return nil
}

func runAppsDomainsVerify(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	domainID := args[1]
	path := fmt.Sprintf("/app-services/%s/domains/%s/verify", appID, domainID)
	if err := edgePostNoBody(path); err != nil {
		return err
	}

	if jsonOut {
		fmt.Println(`{"status":"accepted"}`)
		return nil
	}

	fmt.Printf("Verification queued for domain %q.\n", domainID)
	fmt.Printf("Use 'fdb apps domains list %s' to check when the domain reaches active status.\n", args[0])
	return nil
}

func runAppsDomainsRemove(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	domainID := args[1]
	path := fmt.Sprintf("/app-services/%s/domains/%s", appID, domainID)
	if err := edgeDelete(path); err != nil {
		return err
	}

	if jsonOut {
		fmt.Println(`{"status":"deleted"}`)
		return nil
	}

	fmt.Printf("Domain %q removed.\n", domainID)
	return nil
}

func runAppsEdgeStatus(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var status edgeStatus
	if err := edgeGet(fmt.Sprintf("/app-services/%s/edge", appID), &status); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(status)
	}

	enabled := "no"
	if status.EdgeEnabled {
		enabled = "yes"
	}
	fmt.Printf("Edge enabled:   %s\n", enabled)
	if status.HomePoP != "" {
		fmt.Printf("Home PoP:       %s\n", status.HomePoP)
	}
	if status.CNAMETarget != "" {
		fmt.Printf("CNAME target:   %s\n", status.CNAMETarget)
	}
	fmt.Printf("Config version: %d\n", status.ConfigVersion)

	if len(status.Applications) > 0 {
		fmt.Println("\nPer-PoP convergence:")
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"ZONE", "STATUS", "APPLIED VERSION", "ERROR"})
		table.SetBorder(false)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
		table.SetAlignment(tablewriter.ALIGN_LEFT)
		table.SetCenterSeparator("")
		table.SetColumnSeparator("  ")
		table.SetRowSeparator("")
		table.SetHeaderLine(false)
		table.SetTablePadding("  ")
		table.SetNoWhiteSpace(true)

		for _, a := range status.Applications {
			table.Append([]string{
				a.Zone,
				a.Status,
				fmt.Sprintf("%d", a.AppliedVersion),
				a.ErrorMessage,
			})
		}
		table.Render()
	}
	return nil
}

func runAppsEdgeUpdate(cmd *cobra.Command, args []string) error {
	cachePrefix, _ := cmd.Flags().GetString("cache-path-prefix")
	cacheTTL, _ := cmd.Flags().GetInt("cache-ttl")
	rlRPS, _ := cmd.Flags().GetInt("rate-limit-rps")
	rlBurst, _ := cmd.Flags().GetInt("rate-limit-burst")
	rlKey, _ := cmd.Flags().GetString("rate-limit-key")
	wafMode, _ := cmd.Flags().GetString("waf-mode")

	// Validate flag combinations.
	if cachePrefix != "" && cacheTTL <= 0 {
		return fmt.Errorf("--cache-ttl must be a positive integer when --cache-path-prefix is set")
	}
	if rlRPS > 0 && rlBurst <= 0 {
		return fmt.Errorf("--rate-limit-burst must be a positive integer when --rate-limit-rps is set")
	}
	if wafMode != "" && wafMode != "off" && wafMode != "detect" {
		return fmt.Errorf("--waf-mode must be 'off' or 'detect'")
	}
	if rlKey != "ip" && rlKey != "api_key" {
		return fmt.Errorf("--rate-limit-key must be 'ip' or 'api_key'")
	}

	req := map[string]interface{}{}

	if cachePrefix != "" {
		req["cache_rules"] = []map[string]interface{}{
			{"path_prefix": cachePrefix, "ttl_seconds": cacheTTL},
		}
	}
	if rlRPS > 0 {
		req["rate_limit"] = map[string]interface{}{
			"requests_per_second": rlRPS,
			"burst":               rlBurst,
			"key":                 rlKey,
		}
	}
	if wafMode != "" {
		req["waf_mode"] = wafMode
	}

	if len(req) == 0 {
		return fmt.Errorf("at least one of --cache-path-prefix, --rate-limit-rps, or --waf-mode must be provided")
	}

	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var settings edgeSettings
	if err := edgePut(fmt.Sprintf("/app-services/%s/edge/settings", appID), req, &settings); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(settings)
	}

	fmt.Printf("Edge settings updated (config version: %d).\n", settings.ConfigVersion)
	fmt.Printf("WAF mode:  %s\n", settings.WAFMode)
	if settings.RateLimit != nil {
		fmt.Printf("Rate limit: %d rps, burst %d, key: %s\n",
			settings.RateLimit.RequestsPerSecond, settings.RateLimit.Burst, settings.RateLimit.Key)
	}
	if len(settings.CacheRules) > 0 {
		fmt.Printf("Cache rules:\n")
		for _, r := range settings.CacheRules {
			fmt.Printf("  %s  ->  %ds TTL\n", r.PathPrefix, r.TTLSeconds)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// edgeGetAPIParams resolves current credentials and base URL.
func edgeGetAPIParams() (baseURL, user, pass, org string) {
	baseURL = viper.GetString("api_url")
	user = viper.GetString("username")
	pass = viper.GetString("password")
	org = viper.GetString("org")
	if apiURL != "" {
		baseURL = apiURL
	}
	if username != "" {
		user = username
	}
	if password != "" {
		pass = password
	}
	if orgID != "" {
		org = orgID
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return
}

// edgeDoRequest is a low-level helper that builds, sends, and checks a request.
// body may be nil for GET/DELETE. It reads the full response body and returns it.
func edgeDoRequest(method, path string, body interface{}) ([]byte, error) {
	baseURL, user, pass, org := edgeGetAPIParams()

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if org != "" {
		req.Header.Set("X-Active-Org-ID", org)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// edgeGet fetches the given path and JSON-decodes the response into dest.
func edgeGet(path string, dest interface{}) error {
	data, err := edgeDoRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// edgePost posts body to path and JSON-decodes the response into dest.
func edgePost(path string, body, dest interface{}) error {
	data, err := edgeDoRequest(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// edgePostNoBody posts to path with no request body and ignores the response body.
func edgePostNoBody(path string) error {
	_, err := edgeDoRequest(http.MethodPost, path, nil)
	return err
}

// edgePut puts body to path and JSON-decodes the response into dest.
func edgePut(path string, body, dest interface{}) error {
	data, err := edgeDoRequest(http.MethodPut, path, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// edgeDelete sends a DELETE to path and ignores the response body.
func edgeDelete(path string) error {
	_, err := edgeDoRequest(http.MethodDelete, path, nil)
	return err
}
