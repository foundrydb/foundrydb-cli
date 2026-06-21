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
// Local types mirroring the compliance API surface.
// ---------------------------------------------------------------------------

type complianceReport struct {
	ID              string `json:"id"`
	Framework       string `json:"framework"`
	GeneratedAt     string `json:"generated_at"`
	SigningKeyID    string `json:"signing_key_id"`
	SignatureStatus string `json:"signature_status"`
	DownloadURL    string `json:"download_url,omitempty"`
}

type complianceReportList struct {
	Reports []complianceReport `json:"reports"`
}

type complianceGenerateResponse struct {
	ID              string `json:"id"`
	Framework       string `json:"framework"`
	SigningKeyID    string `json:"signing_key_id"`
	SignatureStatus string `json:"signature_status"`
}

type complianceSigningKey struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Active    bool   `json:"active"`
}

type complianceSigningKeysResponse struct {
	Keys []complianceSigningKey `json:"keys"`
}

// ---------------------------------------------------------------------------
// Command tree
// ---------------------------------------------------------------------------

var complianceCmd = &cobra.Command{
	Use:   "compliance",
	Short: "Generate and download compliance evidence packets (SOC2, GDPR Art. 30 ROPA)",
}

var complianceGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a signed compliance report for an organization",
	RunE:  runComplianceGenerate,
}

var complianceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List compliance reports for an organization",
	RunE:  runComplianceList,
}

var complianceDownloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download a compliance report (JSON or PDF)",
	RunE:  runComplianceDownload,
}

var complianceKeysCmd = &cobra.Command{
	Use:   "keys",
	Short: "List published compliance signing keys (Ed25519)",
	RunE:  runComplianceKeys,
}

func init() {
	// generate flags
	complianceGenerateCmd.Flags().String("org", "", "Organization UUID or slug (required)")
	complianceGenerateCmd.Flags().String("framework", "", "Compliance framework: soc2 or gdpr_ropa (required)")

	// list flags
	complianceListCmd.Flags().String("org", "", "Organization UUID or slug (required)")

	// download flags
	complianceDownloadCmd.Flags().String("org", "", "Organization UUID or slug (required)")
	complianceDownloadCmd.Flags().String("report", "", "Report ID (required)")
	complianceDownloadCmd.Flags().Bool("pdf", false, "Download PDF variant instead of JSON")
	complianceDownloadCmd.Flags().StringP("out", "o", "", "Output file path (defaults to stdout)")

	complianceCmd.AddCommand(complianceGenerateCmd)
	complianceCmd.AddCommand(complianceListCmd)
	complianceCmd.AddCommand(complianceDownloadCmd)
	complianceCmd.AddCommand(complianceKeysCmd)
}

// ---------------------------------------------------------------------------
// Command implementations
// ---------------------------------------------------------------------------

func runComplianceGenerate(cmd *cobra.Command, args []string) error {
	org, _ := cmd.Flags().GetString("org")
	framework, _ := cmd.Flags().GetString("framework")

	if org == "" {
		org = viper.GetString("org")
		if orgID != "" {
			org = orgID
		}
	}
	if org == "" {
		return fmt.Errorf("--org is required")
	}
	if framework == "" {
		return fmt.Errorf("--framework is required (soc2 or gdpr_ropa)")
	}
	if framework != "soc2" && framework != "gdpr_ropa" {
		return fmt.Errorf("--framework must be 'soc2' or 'gdpr_ropa'")
	}

	body := map[string]string{"framework": framework}
	var resp complianceGenerateResponse
	if err := compliancePost(fmt.Sprintf("/organizations/%s/compliance-reports", org), body, &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	fmt.Printf("Report ID:        %s\n", resp.ID)
	fmt.Printf("Framework:        %s\n", resp.Framework)
	fmt.Printf("Signing key ID:   %s\n", resp.SigningKeyID)
	fmt.Printf("Signature status: %s\n", resp.SignatureStatus)
	return nil
}

func runComplianceList(cmd *cobra.Command, args []string) error {
	org, _ := cmd.Flags().GetString("org")

	if org == "" {
		org = viper.GetString("org")
		if orgID != "" {
			org = orgID
		}
	}
	if org == "" {
		return fmt.Errorf("--org is required")
	}

	var resp complianceReportList
	if err := complianceGet(fmt.Sprintf("/organizations/%s/compliance-reports", org), &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	if len(resp.Reports) == 0 {
		fmt.Println("No compliance reports found.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "FRAMEWORK", "GENERATED AT", "SIGNING KEY ID", "STATUS"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, r := range resp.Reports {
		shortID := r.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		shortKeyID := r.SigningKeyID
		if len(shortKeyID) > 16 {
			shortKeyID = shortKeyID[:16]
		}
		generatedAt := r.GeneratedAt
		if len(generatedAt) > 19 {
			generatedAt = generatedAt[:19]
		}
		table.Append([]string{shortID, r.Framework, generatedAt, shortKeyID, r.SignatureStatus})
	}
	table.Render()
	fmt.Printf("\nTotal: %d report(s)\n", len(resp.Reports))
	return nil
}

func runComplianceDownload(cmd *cobra.Command, args []string) error {
	org, _ := cmd.Flags().GetString("org")
	reportID, _ := cmd.Flags().GetString("report")
	pdf, _ := cmd.Flags().GetBool("pdf")
	outFile, _ := cmd.Flags().GetString("out")

	if org == "" {
		org = viper.GetString("org")
		if orgID != "" {
			org = orgID
		}
	}
	if org == "" {
		return fmt.Errorf("--org is required")
	}
	if reportID == "" {
		return fmt.Errorf("--report is required")
	}

	var path string
	if pdf {
		path = fmt.Sprintf("/organizations/%s/compliance-reports/%s/pdf", org, reportID)
	} else {
		path = fmt.Sprintf("/organizations/%s/compliance-reports/%s", org, reportID)
	}

	data, err := complianceDoRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}

	if outFile != "" {
		if err := os.WriteFile(outFile, data, 0644); err != nil {
			return fmt.Errorf("write output file: %w", err)
		}
		if !jsonOut {
			fmt.Printf("Report written to %s (%d bytes).\n", outFile, len(data))
		}
		return nil
	}

	_, err = os.Stdout.Write(data)
	return err
}

func runComplianceKeys(cmd *cobra.Command, args []string) error {
	// This endpoint is public; no org header required.
	var resp complianceSigningKeysResponse
	if err := complianceGet("/.well-known/compliance-signing-keys", &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	if len(resp.Keys) == 0 {
		fmt.Println("No compliance signing keys published.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"KEY ID", "ALGORITHM", "ACTIVE", "CREATED AT", "EXPIRES AT"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, k := range resp.Keys {
		active := "no"
		if k.Active {
			active = "yes"
		}
		createdAt := k.CreatedAt
		if len(createdAt) > 19 {
			createdAt = createdAt[:19]
		}
		expiresAt := "-"
		if k.ExpiresAt != "" {
			expiresAt = k.ExpiresAt
			if len(expiresAt) > 19 {
				expiresAt = expiresAt[:19]
			}
		}
		table.Append([]string{k.KeyID, k.Algorithm, active, createdAt, expiresAt})
	}
	table.Render()
	fmt.Printf("\nTotal: %d key(s)\n", len(resp.Keys))
	return nil
}

// ---------------------------------------------------------------------------
// HTTP helpers (follow the same pattern as edgeDoRequest in edge.go).
// ---------------------------------------------------------------------------

func complianceGetAPIParams() (baseURL, user, pass, org string) {
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

// complianceDoRequest builds, sends, and returns the raw response bytes.
func complianceDoRequest(method, path string, body interface{}) ([]byte, error) {
	baseURL, user, pass, org := complianceGetAPIParams()

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

func complianceGet(path string, dest interface{}) error {
	data, err := complianceDoRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func compliancePost(path string, body, dest interface{}) error {
	data, err := complianceDoRequest(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
