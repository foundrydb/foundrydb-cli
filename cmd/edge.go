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

var appsEdgeSettingsGetCmd = &cobra.Command{
	Use:   "get-settings <app-id>",
	Short: "Show the customer-tunable edge settings stored for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeSettingsGet,
}

var appsEdgeUpdateCmd = &cobra.Command{
	Use:   "update-settings <app-id>",
	Short: "Update edge settings for an app service",
	Long: `Update the customer-tunable edge settings for an app service.

Simple settings can be set with the typed flags below. Anything the typed
flags do not cover (and every net-new access/auth and security-hardening
setting) is set by supplying a JSON document that is sent verbatim as the
PUT body, either with --settings-file <path> or --settings-json '<json>'.
A settings JSON document is merged on top of any typed flags (typed flags
win on key conflicts).

Net-new fields you can include in the settings JSON document:

  Cache depth (inside each cache_rules entry, alongside path_prefix/ttl_seconds):
    stale_while_revalidate_seconds, stale_if_error_seconds, request_collapsing,
    cache_key { vary_query_params[], vary_headers[], vary_cookies[] }

  Access / auth (top-level):
    jwt_auth      { enabled, paths[], jwks_url, public_keys[], issuer,
                    audiences[], required_claims[{name,value}], forward_claims_header }
    signed_urls   { enabled, paths[], secret_name, ttl_seconds,
                    signature_param, expires_param }
    api_key_auth  { enabled, paths[], key_location("header"|"query"),
                    key_name, keys[{name, key, rate_tier{...}}] }
      (an api key's "key" is write-only plaintext, hashed server-side and never echoed)

  Security hardening (top-level):
    waf_paranoia_level   int 1..4 (0 = platform default PL1)
    waf_rule_exclusions  [{rule_id, target}]
    ddos_profile   { enabled, per_ip_requests_per_second, per_ip_burst, per_ip_conn_cap }
    bot_management { enabled, action("log"|"block"|"challenge"),
                     known_bad_bots, rate_based_heuristic }
    ato_protection { enabled, auth_paths[], failure_status_codes[],
                     per_ip_threshold_per_min, per_username_threshold_per_min,
                     username_field, action("alert"|"ratelimit"|"lock") }

Examples:
  # Cache /static for 1h with stale-while-revalidate and request collapsing:
  fdb apps edge update-settings my-app --settings-json '{
    "cache_rules":[{"path_prefix":"/static","ttl_seconds":3600,
      "stale_while_revalidate_seconds":60,"request_collapsing":true,
      "cache_key":{"vary_query_params":["v"],"vary_headers":["Accept-Encoding"]}}]}'

  # Require JWTs on /api and turn WAF up to paranoia level 2 with a DDoS profile:
  fdb apps edge update-settings my-app --settings-file edge.json
  # edge.json:
  # {"waf_paranoia_level":2,
  #  "jwt_auth":{"enabled":true,"paths":["/api/"],"jwks_url":"https://idp/.well-known/jwks.json","issuer":"https://idp"},
  #  "ddos_profile":{"enabled":true,"per_ip_requests_per_second":50,"per_ip_burst":100}}

  # Protect login endpoints from credential stuffing (ATO):
  fdb apps edge update-settings my-app --settings-json '{
    "ato_protection":{"enabled":true,"auth_paths":["/login"],
      "per_ip_threshold_per_min":20,"action":"ratelimit"}}'`,
	Args: cobra.ExactArgs(1),
	RunE: runAppsEdgeUpdate,
}

var appsEdgeRolloutCmd = &cobra.Command{
	Use:   "rollout",
	Short: "Inspect and drive staged edge config rollouts",
}

var appsEdgeRolloutGetCmd = &cobra.Command{
	Use:   "get <app-id>",
	Short: "Show the current (or most recent) staged edge config rollout",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeRolloutGet,
}

var appsEdgeRolloutPromoteCmd = &cobra.Command{
	Use:   "promote <app-id>",
	Short: "Promote a holding canary rollout to the rest of the fleet",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeRolloutPromote,
}

var appsEdgeRolloutAbortCmd = &cobra.Command{
	Use:   "abort <app-id>",
	Short: "Abort an active staged rollout (the rest of the fleet keeps the prior version)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeRolloutAbort,
}

var appsEdgeVersionsCmd = &cobra.Command{
	Use:   "versions <app-id>",
	Short: "List the append-only edge config version history for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeVersions,
}

var appsEdgeRollbackCmd = &cobra.Command{
	Use:   "rollback <app-id>",
	Short: "Roll an app's edge config back to a prior version",
	Long: `Roll an app service's edge config back to a prior version.

Supply exactly one of --to-version <n> or --previous. The rollback restores
that version's customer-settable subset as a NEW forward version; it never
mutates the version history.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppsEdgeRollback,
}

var appsEdgeCachePurgeCmd = &cobra.Command{
	Use:   "cache-purge <app-id>",
	Short: "Purge the app's edge cache across its serving PoP nodes",
	Long: `Purge the app's edge cache across its serving PoP nodes.

Supply exactly one of --all or one or more --path <abs-path>. The purge rolls
across nodes one at a time in the background, so the response reports the plan
(planned node count) rather than the completed result.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppsEdgeCachePurge,
}

var appsEdgeAnalyticsCmd = &cobra.Command{
	Use:   "analytics <app-id>",
	Short: "Show the edge analytics summary for an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeAnalytics,
}

var appsEdgeLogDrainsCmd = &cobra.Command{
	Use:   "log-drains",
	Short: "Manage edge access-log drains for an app service",
}

var appsEdgeLogDrainsListCmd = &cobra.Command{
	Use:   "list <app-id>",
	Short: "List the app's edge access-log drains",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsEdgeLogDrainsList,
}

var appsEdgeLogDrainsCreateCmd = &cobra.Command{
	Use:   "create <app-id>",
	Short: "Create an edge access-log drain from a JSON document",
	Long: `Create an edge access-log drain for an app service.

The drain definition is supplied as a JSON document with --file <path> or
--json '<json>'. Configuration is destination-specific, for example:
  s3:      {"name":"...","destination_type":"s3",
            "configuration":{"endpoint":"...","region":"...","bucket":"...",
              "prefix":"...","access_key_id":"...","secret_access_key":"..."}}
  webhook: {"name":"...","destination_type":"webhook",
            "configuration":{"url":"...","auth_header_name":"...","auth_header_value":"..."}}
An optional redaction_policy { ip_mode, ip_hash_salt, strip_query_string,
header_allow_list[] } controls per-line privacy.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppsEdgeLogDrainsCreate,
}

var appsEdgeLogDrainsDeleteCmd = &cobra.Command{
	Use:   "delete <app-id> <drain-id>",
	Short: "Delete an edge access-log drain",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppsEdgeLogDrainsDelete,
}

var appsEdgeLogDrainsTestCmd = &cobra.Command{
	Use:   "test <app-id> <drain-id>",
	Short: "Test connectivity to an edge access-log drain's destination",
	Args:  cobra.ExactArgs(2),
	RunE:  runAppsEdgeLogDrainsTest,
}

func init() {
	// domains add flags
	appsDomainsAddCmd.Flags().String("domain", "", "Custom domain name to add (required, e.g. app.example.com)")

	// edge update-settings flags
	appsEdgeUpdateCmd.Flags().String("cache-path-prefix", "", "Cache rule: path prefix to cache (e.g. /static)")
	appsEdgeUpdateCmd.Flags().Int("cache-ttl", 0, "Cache rule: TTL in seconds (required when --cache-path-prefix is set)")
	appsEdgeUpdateCmd.Flags().Int("cache-stale-while-revalidate", 0, "Cache rule: serve stale for N seconds while revalidating (cache depth)")
	appsEdgeUpdateCmd.Flags().Int("cache-stale-if-error", 0, "Cache rule: serve stale for N seconds if the origin errors (cache depth)")
	appsEdgeUpdateCmd.Flags().Bool("cache-request-collapsing", false, "Cache rule: collapse concurrent misses into one origin fetch (cache depth)")
	appsEdgeUpdateCmd.Flags().Int("rate-limit-rps", 0, "Rate limit: requests per second (omit to leave unchanged)")
	appsEdgeUpdateCmd.Flags().Int("rate-limit-burst", 0, "Rate limit: burst allowance (required when --rate-limit-rps is set)")
	appsEdgeUpdateCmd.Flags().String("rate-limit-key", "ip", "Rate limit: bucket key (ip or api_key)")
	appsEdgeUpdateCmd.Flags().String("waf-mode", "", "WAF mode: off, detect, or block (omit to leave unchanged)")
	appsEdgeUpdateCmd.Flags().Int("waf-paranoia-level", 0, "WAF paranoia level 1..4 (0 = platform default PL1; security hardening)")
	appsEdgeUpdateCmd.Flags().String("settings-file", "", "Path to a JSON document sent verbatim as the settings body (covers every net-new field)")
	appsEdgeUpdateCmd.Flags().String("settings-json", "", "Inline JSON document sent verbatim as the settings body (covers every net-new field)")

	// edge rollback flags
	appsEdgeRollbackCmd.Flags().Int64("to-version", 0, "Roll back to this explicit config version")
	appsEdgeRollbackCmd.Flags().Bool("previous", false, "Roll back to the version immediately before the active one")

	// edge rollout abort flags
	appsEdgeRolloutAbortCmd.Flags().String("reason", "", "Optional operator note recorded as the abort reason")

	// edge cache-purge flags
	appsEdgeCachePurgeCmd.Flags().Bool("all", false, "Purge every cached entry for the app")
	appsEdgeCachePurgeCmd.Flags().StringSlice("path", nil, "Absolute path whose cached entries to invalidate (repeatable)")

	// edge analytics flags
	appsEdgeAnalyticsCmd.Flags().Int("window-minutes", 0, "Analytics window in minutes (0 = server default of 60)")

	// edge log-drains create flags
	appsEdgeLogDrainsCreateCmd.Flags().String("file", "", "Path to a JSON log-drain definition")
	appsEdgeLogDrainsCreateCmd.Flags().String("json", "", "Inline JSON log-drain definition")

	appsDomainsCmd.AddCommand(appsDomainsListCmd)
	appsDomainsCmd.AddCommand(appsDomainsAddCmd)
	appsDomainsCmd.AddCommand(appsDomainsVerifyCmd)
	appsDomainsCmd.AddCommand(appsDomainsRemoveCmd)

	appsEdgeRolloutCmd.AddCommand(appsEdgeRolloutGetCmd)
	appsEdgeRolloutCmd.AddCommand(appsEdgeRolloutPromoteCmd)
	appsEdgeRolloutCmd.AddCommand(appsEdgeRolloutAbortCmd)

	appsEdgeLogDrainsCmd.AddCommand(appsEdgeLogDrainsListCmd)
	appsEdgeLogDrainsCmd.AddCommand(appsEdgeLogDrainsCreateCmd)
	appsEdgeLogDrainsCmd.AddCommand(appsEdgeLogDrainsDeleteCmd)
	appsEdgeLogDrainsCmd.AddCommand(appsEdgeLogDrainsTestCmd)

	appsEdgeCmd.AddCommand(appsEdgeStatusCmd)
	appsEdgeCmd.AddCommand(appsEdgeSettingsGetCmd)
	appsEdgeCmd.AddCommand(appsEdgeUpdateCmd)
	appsEdgeCmd.AddCommand(appsEdgeVersionsCmd)
	appsEdgeCmd.AddCommand(appsEdgeRollbackCmd)
	appsEdgeCmd.AddCommand(appsEdgeRolloutCmd)
	appsEdgeCmd.AddCommand(appsEdgeCachePurgeCmd)
	appsEdgeCmd.AddCommand(appsEdgeAnalyticsCmd)
	appsEdgeCmd.AddCommand(appsEdgeLogDrainsCmd)

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
	cacheSWR, _ := cmd.Flags().GetInt("cache-stale-while-revalidate")
	cacheSIE, _ := cmd.Flags().GetInt("cache-stale-if-error")
	cacheCollapse, _ := cmd.Flags().GetBool("cache-request-collapsing")
	rlRPS, _ := cmd.Flags().GetInt("rate-limit-rps")
	rlBurst, _ := cmd.Flags().GetInt("rate-limit-burst")
	rlKey, _ := cmd.Flags().GetString("rate-limit-key")
	wafMode, _ := cmd.Flags().GetString("waf-mode")
	wafParanoia, _ := cmd.Flags().GetInt("waf-paranoia-level")
	settingsFile, _ := cmd.Flags().GetString("settings-file")
	settingsJSON, _ := cmd.Flags().GetString("settings-json")

	// Validate flag combinations.
	if cachePrefix != "" && cacheTTL <= 0 {
		return fmt.Errorf("--cache-ttl must be a positive integer when --cache-path-prefix is set")
	}
	if rlRPS > 0 && rlBurst <= 0 {
		return fmt.Errorf("--rate-limit-burst must be a positive integer when --rate-limit-rps is set")
	}
	if wafMode != "" && wafMode != "off" && wafMode != "detect" && wafMode != "block" {
		return fmt.Errorf("--waf-mode must be 'off', 'detect', or 'block'")
	}
	if rlKey != "ip" && rlKey != "api_key" {
		return fmt.Errorf("--rate-limit-key must be 'ip' or 'api_key'")
	}
	if wafParanoia < 0 || wafParanoia > 4 {
		return fmt.Errorf("--waf-paranoia-level must be between 1 and 4 (or 0 for the platform default)")
	}
	if settingsFile != "" && settingsJSON != "" {
		return fmt.Errorf("supply only one of --settings-file or --settings-json")
	}

	// Start from the verbatim settings document (if any), then layer typed
	// flags on top so they win on key conflicts.
	req := map[string]interface{}{}
	if settingsFile != "" || settingsJSON != "" {
		raw := []byte(settingsJSON)
		if settingsFile != "" {
			data, err := os.ReadFile(settingsFile)
			if err != nil {
				return fmt.Errorf("read --settings-file: %w", err)
			}
			raw = data
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return fmt.Errorf("parse settings JSON: %w", err)
		}
	}

	if cachePrefix != "" {
		rule := map[string]interface{}{"path_prefix": cachePrefix, "ttl_seconds": cacheTTL}
		if cacheSWR > 0 {
			rule["stale_while_revalidate_seconds"] = cacheSWR
		}
		if cacheSIE > 0 {
			rule["stale_if_error_seconds"] = cacheSIE
		}
		if cacheCollapse {
			rule["request_collapsing"] = true
		}
		req["cache_rules"] = []map[string]interface{}{rule}
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
	if wafParanoia > 0 {
		req["waf_paranoia_level"] = wafParanoia
	}

	if len(req) == 0 {
		return fmt.Errorf("provide at least one setting (typed flags, --settings-file, or --settings-json)")
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

func runAppsEdgeSettingsGet(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	// Decode into a generic map so every net-new field is shown without the
	// CLI having to mirror the full settings schema.
	var settings map[string]interface{}
	if err := edgeGet(fmt.Sprintf("/app-services/%s/edge/settings", appID), &settings); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(settings)
	}

	if v, ok := settings["config_version"]; ok {
		fmt.Printf("Config version: %v\n", v)
	}
	if v, ok := settings["waf_mode"]; ok && v != "" {
		fmt.Printf("WAF mode:       %v\n", v)
	}
	if v, ok := settings["waf_paranoia_level"]; ok {
		fmt.Printf("WAF paranoia:   %v\n", v)
	}
	// Surface the remaining settings as indented JSON so net-new structured
	// fields (jwt_auth, ddos_profile, cache depth, etc.) are always visible.
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println("\nSettings document:")
	fmt.Println(string(out))
	return nil
}

func runAppsEdgeVersions(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var resp struct {
		ActiveVersion int64 `json:"active_version"`
		Versions      []struct {
			Version        int64   `json:"version"`
			ConfigHash     string  `json:"config_hash"`
			Source         string  `json:"source"`
			Active         bool    `json:"active"`
			RolledBackFrom *int64  `json:"rolled_back_from,omitempty"`
			CreatedBy      *string `json:"created_by,omitempty"`
			CreatedAt      string  `json:"created_at"`
		} `json:"versions"`
	}
	if err := edgeGet(fmt.Sprintf("/app-services/%s/edge/versions", appID), &resp); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(resp)
	}

	fmt.Printf("Active version: %d\n\n", resp.ActiveVersion)
	if len(resp.Versions) == 0 {
		fmt.Println("No version history.")
		return nil
	}
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"VERSION", "ACTIVE", "SOURCE", "ROLLED BACK FROM", "CREATED"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)
	for _, v := range resp.Versions {
		active := ""
		if v.Active {
			active = "yes"
		}
		rbf := ""
		if v.RolledBackFrom != nil {
			rbf = fmt.Sprintf("%d", *v.RolledBackFrom)
		}
		created := v.CreatedAt
		if len(created) > 19 {
			created = created[:19]
		}
		table.Append([]string{fmt.Sprintf("%d", v.Version), active, v.Source, rbf, created})
	}
	table.Render()
	return nil
}

func runAppsEdgeRollback(cmd *cobra.Command, args []string) error {
	toVersion, _ := cmd.Flags().GetInt64("to-version")
	previous, _ := cmd.Flags().GetBool("previous")

	if (toVersion > 0) == previous {
		return fmt.Errorf("supply exactly one of --to-version <n> or --previous")
	}

	body := map[string]interface{}{}
	if previous {
		body["to"] = "previous"
	} else {
		body["to_version"] = toVersion
	}

	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var resp struct {
		ActiveVersion  int64  `json:"active_version"`
		RolledBackFrom int64  `json:"rolled_back_from"`
		Source         string `json:"source"`
	}
	if err := edgePost(fmt.Sprintf("/app-services/%s/edge/rollback", appID), body, &resp); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(resp)
	}
	fmt.Printf("Rolled back from version %d. New active version: %d.\n", resp.RolledBackFrom, resp.ActiveVersion)
	fmt.Printf("Use 'fdb apps edge status %s' to watch the fleet converge.\n", args[0])
	return nil
}

func runAppsEdgeRolloutGet(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var resp struct {
		Active  bool `json:"active"`
		Rollout *struct {
			ID             string  `json:"id"`
			TargetVersion  int64   `json:"target_version"`
			Phase          string  `json:"phase"`
			CanaryScope    string  `json:"canary_scope"`
			CanarySelector string  `json:"canary_selector,omitempty"`
			StartedAt      string  `json:"started_at"`
			AbortReason    *string `json:"abort_reason,omitempty"`
		} `json:"rollout,omitempty"`
	}
	if err := edgeGet(fmt.Sprintf("/app-services/%s/edge/rollout", appID), &resp); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(resp)
	}
	if resp.Rollout == nil {
		fmt.Println("No staged rollout has ever run for this app.")
		return nil
	}
	r := resp.Rollout
	fmt.Printf("Rollout:        %s\n", r.ID)
	fmt.Printf("Active:         %t\n", resp.Active)
	fmt.Printf("Phase:          %s\n", r.Phase)
	fmt.Printf("Target version: %d\n", r.TargetVersion)
	fmt.Printf("Canary scope:   %s %s\n", r.CanaryScope, r.CanarySelector)
	fmt.Printf("Started at:     %s\n", r.StartedAt)
	if r.AbortReason != nil && *r.AbortReason != "" {
		fmt.Printf("Abort reason:   %s\n", *r.AbortReason)
	}
	if resp.Active && r.Phase == "canary" {
		fmt.Printf("\nPromote with 'fdb apps edge rollout promote %s' or abort with 'fdb apps edge rollout abort %s'.\n", args[0], args[0])
	}
	return nil
}

func runAppsEdgeRolloutPromote(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}
	if err := edgePostNoBody(fmt.Sprintf("/app-services/%s/edge/rollout/promote", appID)); err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(`{"status":"promoting"}`)
		return nil
	}
	fmt.Println("Rollout promoted; the canary version is fanning out to the rest of the fleet.")
	return nil
}

func runAppsEdgeRolloutAbort(cmd *cobra.Command, args []string) error {
	reason, _ := cmd.Flags().GetString("reason")
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}
	body := map[string]interface{}{}
	if reason != "" {
		body["reason"] = reason
	}
	if _, err := edgeDoRequest(http.MethodPost, fmt.Sprintf("/app-services/%s/edge/rollout/abort", appID), body); err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(`{"status":"aborted"}`)
		return nil
	}
	fmt.Println("Rollout aborted; the rest of the fleet keeps the prior version.")
	return nil
}

func runAppsEdgeCachePurge(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	paths, _ := cmd.Flags().GetStringSlice("path")

	if all == (len(paths) > 0) {
		return fmt.Errorf("supply exactly one of --all or one or more --path")
	}

	body := map[string]interface{}{}
	if all {
		body["all"] = true
	} else {
		body["paths"] = paths
	}

	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var resp struct {
		PlannedNodes int      `json:"planned_nodes"`
		NodeIDs      []string `json:"node_ids,omitempty"`
		Rolling      bool     `json:"rolling"`
	}
	if err := edgePost(fmt.Sprintf("/app-services/%s/edge/cache/purge", appID), body, &resp); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(resp)
	}
	fmt.Printf("Cache purge started across %d node(s) (rolling: %t).\n", resp.PlannedNodes, resp.Rolling)
	return nil
}

func runAppsEdgeAnalytics(cmd *cobra.Command, args []string) error {
	window, _ := cmd.Flags().GetInt("window-minutes")

	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/app-services/%s/edge/analytics", appID)
	if window > 0 {
		path += fmt.Sprintf("?window_minutes=%d", window)
	}

	var resp struct {
		WindowMinutes int `json:"window_minutes"`
		Total         struct {
			RequestsTotal    int64   `json:"requests_total"`
			ErrorRatePct     float64 `json:"error_rate_pct"`
			RateLimitedTotal int64   `json:"rate_limited_total"`
			Cache            struct {
				HitRatio float64 `json:"hit_ratio"`
			} `json:"cache"`
			LatencyMs struct {
				P50 float64 `json:"p50"`
				P95 float64 `json:"p95"`
				P99 float64 `json:"p99"`
			} `json:"latency_ms"`
			WAFDetectionsTotal int64 `json:"waf_detections_total"`
		} `json:"total"`
	}
	if err := edgeGet(path, &resp); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(resp)
	}
	t := resp.Total
	fmt.Printf("Window:           %d min\n", resp.WindowMinutes)
	fmt.Printf("Requests:         %d\n", t.RequestsTotal)
	fmt.Printf("Error rate:       %.2f%%\n", t.ErrorRatePct)
	fmt.Printf("Cache hit ratio:  %.2f\n", t.Cache.HitRatio)
	fmt.Printf("Rate limited:     %d\n", t.RateLimitedTotal)
	fmt.Printf("WAF detections:   %d\n", t.WAFDetectionsTotal)
	fmt.Printf("Latency (ms):     p50 %.1f  p95 %.1f  p99 %.1f\n", t.LatencyMs.P50, t.LatencyMs.P95, t.LatencyMs.P99)
	return nil
}

func runAppsEdgeLogDrainsList(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var resp struct {
		Drains []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			DestinationType string `json:"destination_type"`
			IsEnabled       bool   `json:"is_enabled"`
			LastExportError string `json:"last_export_error,omitempty"`
		} `json:"drains"`
	}
	if err := edgeGet(fmt.Sprintf("/app-services/%s/edge/log-drains", appID), &resp); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(resp)
	}
	if len(resp.Drains) == 0 {
		fmt.Println("No edge log drains configured.")
		return nil
	}
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "NAME", "DESTINATION", "ENABLED", "LAST ERROR"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)
	for _, d := range resp.Drains {
		shortID := d.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		enabled := "no"
		if d.IsEnabled {
			enabled = "yes"
		}
		table.Append([]string{shortID, d.Name, d.DestinationType, enabled, d.LastExportError})
	}
	table.Render()
	return nil
}

func runAppsEdgeLogDrainsCreate(cmd *cobra.Command, args []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	inlineJSON, _ := cmd.Flags().GetString("json")
	if (filePath == "") == (inlineJSON == "") {
		return fmt.Errorf("supply exactly one of --file or --json")
	}

	raw := []byte(inlineJSON)
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read --file: %w", err)
		}
		raw = data
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("parse log-drain JSON: %w", err)
	}

	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	var drain map[string]interface{}
	if err := edgePost(fmt.Sprintf("/app-services/%s/edge/log-drains", appID), body, &drain); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(drain)
	}
	fmt.Printf("Log drain created (ID: %v, name: %v).\n", drain["id"], drain["name"])
	return nil
}

func runAppsEdgeLogDrainsDelete(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}
	if err := edgeDelete(fmt.Sprintf("/app-services/%s/edge/log-drains/%s", appID, args[1])); err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(`{"status":"deleted"}`)
		return nil
	}
	fmt.Printf("Log drain %q deleted.\n", args[1])
	return nil
}

func runAppsEdgeLogDrainsTest(cmd *cobra.Command, args []string) error {
	client := newClient()
	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := edgePost(fmt.Sprintf("/app-services/%s/edge/log-drains/%s/test", appID, args[1]), nil, &resp); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(resp)
	}
	if resp.OK {
		fmt.Println("Drain destination reachable.")
	} else {
		fmt.Printf("Drain destination NOT reachable: %s\n", resp.Error)
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
