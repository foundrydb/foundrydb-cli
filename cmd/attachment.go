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
// Local types mirroring the companion-app attachment API surface.
// ---------------------------------------------------------------------------

type attachmentCatalogEntry struct {
	Kind          string   `json:"kind"`
	DisplayName   string   `json:"display_name"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	DefaultPlan   string   `json:"default_plan"`
	ParentEngines []string `json:"parent_engines"`
}

type attachmentCatalogResponse struct {
	Attachments []attachmentCatalogEntry `json:"attachments"`
}

type attachmentSummary struct {
	AttachmentID string `json:"attachment_id"`
	AppServiceID string `json:"app_service_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	WiringStatus string `json:"wiring_status"`
	URL          string `json:"url,omitempty"`
}

type listAttachmentsResponse struct {
	Attachments []attachmentSummary `json:"attachments"`
}

type createAttachmentRequest struct {
	Kind      string `json:"kind"`
	PlanName  string `json:"plan_name,omitempty"`
	Subdomain string `json:"subdomain,omitempty"`
}

// createdAttachment is the app service returned by POST /managed-services/{id}/attachments.
type createdAttachment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Zone   string `json:"zone,omitempty"`
}

type attachmentCredentials struct {
	AdminEmail    string            `json:"admin_email,omitempty"`
	AdminPassword string            `json:"admin_password,omitempty"`
	Generated     map[string]string `json:"generated,omitempty"`
	LoginURL      string            `json:"login_url,omitempty"`
}

// ---------------------------------------------------------------------------
// Command tree
// ---------------------------------------------------------------------------

var attachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Manage companion-app attachments on database services",
}

var attachmentCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "List installable companion-app kinds (metabase, directus, ...)",
	RunE:  runAttachmentCatalog,
}

var attachmentCreateCmd = &cobra.Command{
	Use:   "create <db-service-id>",
	Short: "Attach a companion app to a database service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAttachmentCreate,
}

var attachmentListCmd = &cobra.Command{
	Use:   "list <db-service-id>",
	Short: "List companion apps attached to a database service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAttachmentList,
}

var attachmentCredentialsCmd = &cobra.Command{
	Use:   "credentials <app-service-id>",
	Short: "Reveal the generated admin credentials for an attached companion app",
	Args:  cobra.ExactArgs(1),
	RunE:  runAttachmentCredentials,
}

func init() {
	attachmentCreateCmd.Flags().String("kind", "", "Companion-app kind from the catalog (required, e.g. metabase)")
	attachmentCreateCmd.Flags().String("plan", "", "Compute plan override (default from catalog)")
	attachmentCreateCmd.Flags().String("subdomain", "", "Subdomain override for the companion app's public URL")

	attachmentCmd.AddCommand(attachmentCatalogCmd)
	attachmentCmd.AddCommand(attachmentCreateCmd)
	attachmentCmd.AddCommand(attachmentListCmd)
	attachmentCmd.AddCommand(attachmentCredentialsCmd)
}

// ---------------------------------------------------------------------------
// Command implementations
// ---------------------------------------------------------------------------

func runAttachmentCatalog(cmd *cobra.Command, args []string) error {
	var resp attachmentCatalogResponse
	if err := attachmentGet("/attachment-catalog", &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	if len(resp.Attachments) == 0 {
		fmt.Println("No attachment kinds available.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"KIND", "DISPLAY NAME", "CATEGORY", "ENGINES", "DESCRIPTION"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, e := range resp.Attachments {
		desc := e.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		engines := strings.Join(e.ParentEngines, ", ")
		table.Append([]string{e.Kind, e.DisplayName, e.Category, engines, desc})
	}
	table.Render()
	fmt.Printf("\nTotal: %d kind(s)\n", len(resp.Attachments))
	return nil
}

func runAttachmentCreate(cmd *cobra.Command, args []string) error {
	dbServiceID := args[0]
	kind, _ := cmd.Flags().GetString("kind")
	plan, _ := cmd.Flags().GetString("plan")
	subdomain, _ := cmd.Flags().GetString("subdomain")

	if kind == "" {
		return fmt.Errorf("--kind is required (run 'fdb attachment catalog' to see available kinds)")
	}

	body := createAttachmentRequest{
		Kind:      kind,
		PlanName:  plan,
		Subdomain: subdomain,
	}

	fmt.Printf("Attaching %q to database service %s...\n", kind, dbServiceID)

	var app createdAttachment
	if err := attachmentPost("/managed-services/"+dbServiceID+"/attachments", body, &app); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(app)
	}

	fmt.Printf("Attachment created successfully.\n")
	fmt.Printf("  App service ID: %s\n", app.ID)
	fmt.Printf("  Name:           %s\n", app.Name)
	fmt.Printf("  Status:         %s\n", app.Status)
	fmt.Printf("\nUse 'fdb attachment list %s' to monitor provisioning.\n", dbServiceID)
	fmt.Printf("Use 'fdb attachment credentials %s' to reveal admin credentials once Running.\n", app.ID)
	return nil
}

func runAttachmentList(cmd *cobra.Command, args []string) error {
	dbServiceID := args[0]

	var resp listAttachmentsResponse
	if err := attachmentGet("/managed-services/"+dbServiceID+"/attachments", &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	if len(resp.Attachments) == 0 {
		fmt.Printf("No attachments found for service %s.\n", dbServiceID)
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"KIND", "NAME", "STATUS", "WIRING", "URL"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, a := range resp.Attachments {
		url := a.URL
		if url == "" {
			url = "-"
		}
		table.Append([]string{a.Kind, a.Name, a.Status, a.WiringStatus, url})
	}
	table.Render()
	fmt.Printf("\nTotal: %d attachment(s)\n", len(resp.Attachments))
	return nil
}

func runAttachmentCredentials(cmd *cobra.Command, args []string) error {
	appServiceID := args[0]

	var creds attachmentCredentials
	if err := attachmentGet("/app-services/"+appServiceID+"/attachment-credentials", &creds); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(creds)
	}

	if creds.LoginURL != "" {
		fmt.Printf("Login URL:      %s\n", creds.LoginURL)
	}
	if creds.AdminEmail != "" {
		fmt.Printf("Admin email:    %s\n", creds.AdminEmail)
	}
	if creds.AdminPassword != "" {
		fmt.Printf("Admin password: %s\n", creds.AdminPassword)
	}
	if len(creds.Generated) > 0 {
		fmt.Printf("Generated credentials:\n")
		for k, v := range creds.Generated {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP helpers (follow the same pattern as complianceDoRequest in compliance.go).
// ---------------------------------------------------------------------------

func attachmentGetAPIParams() (baseURL, user, pass, org string) {
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

func attachmentDoRequest(method, path string, body interface{}) ([]byte, error) {
	baseURL, user, pass, org := attachmentGetAPIParams()

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

func attachmentGet(path string, dest interface{}) error {
	data, err := attachmentDoRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func attachmentPost(path string, body, dest interface{}) error {
	data, err := attachmentDoRequest(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
