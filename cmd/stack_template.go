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
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ---------------------------------------------------------------------------
// Local types for Phase 2 marketplace template API surface.
// These mirror the SDK v0.8.0 types; the CLI builds against v0.7.0 by using
// direct HTTP calls rather than SDK methods for the new endpoints.
// ---------------------------------------------------------------------------

type customerStackTemplate struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	DisplayName    string    `json:"display_name"`
	Description    string    `json:"description"`
	Version        string    `json:"version"`
	Visibility     string    `json:"visibility"`
	Published      bool      `json:"published"`
	OrganizationID string    `json:"organization_id,omitempty"`
	Descriptor     string    `json:"descriptor,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type listCustomerTemplatesResponse struct {
	Templates []customerStackTemplate `json:"templates"`
}

type createStackTemplateRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
	Visibility  string `json:"visibility,omitempty"`
	Descriptor  string `json:"descriptor"`
}

// stackMktCostPreview is a subset of StackCostPreview used for marketplace preview calls.
type stackMktCostPreview struct {
	TemplateName string `json:"template_name"`
	Currency     string `json:"currency"`
	MonthlyTotal float64 `json:"monthly_total"`
	LineItems    []struct {
		SymbolicName string  `json:"symbolic_name"`
		Kind         string  `json:"kind"`
		Description  string  `json:"description"`
		MonthlyCost  float64 `json:"monthly_cost"`
		IsCeiling    bool    `json:"is_ceiling,omitempty"`
	} `json:"line_items"`
	Warnings []string `json:"warnings,omitempty"`
}

// stackMktStack is a thin Stack type for marketplace launch responses.
type stackMktStack struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TemplateName string `json:"template_name"`
	Status       string `json:"status"`
}

// ---------------------------------------------------------------------------
// Command tree: fdb stack template
// ---------------------------------------------------------------------------

var stackTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage customer-authored and marketplace stack templates",
	Long: `Create, publish, and manage stack templates in the FoundryDB marketplace.

Use "fdb stack template create" to upload a descriptor YAML or JSON file as a
new template, "fdb stack template list" to browse templates you own, and
"fdb stack template marketplace" to discover public marketplace templates.`,
}

var stackTemplateCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new customer-authored stack template",
	Long: `Upload a stack descriptor file as a new customer-authored template.

The descriptor must be a valid YAML or JSON stack definition. The server
validates the descriptor and returns an error if it is malformed.

Example:
  fdb stack template create -f my-stack.yaml --name my-rag-stack --visibility org_shared`,
	RunE: runStackTemplateCreate,
}

var stackTemplateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stack templates you own",
	RunE:  runStackTemplateList,
}

var stackTemplateMarketplaceCmd = &cobra.Command{
	Use:   "marketplace",
	Short: "List published marketplace stack templates",
	RunE:  runStackTemplateMarketplace,
}

var stackTemplateGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get details of a customer-authored stack template",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackTemplateGet,
}

var stackTemplateDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a customer-authored stack template",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackTemplateDelete,
}

var stackTemplatePublishCmd = &cobra.Command{
	Use:   "publish <id>",
	Short: "Publish a stack template to the marketplace",
	Long: `Mark a customer-authored template as published so it appears in the marketplace.

The template must have visibility=public; use "fdb stack template create" with
--visibility public or update the visibility before publishing.`,
	Args: cobra.ExactArgs(1),
	RunE: runStackTemplatePublish,
}

var stackTemplateUnpublishCmd = &cobra.Command{
	Use:   "unpublish <id>",
	Short: "Remove a stack template from the marketplace (without deleting it)",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackTemplateUnpublish,
}

func init() {
	stackTemplateCreateCmd.Flags().StringP("file", "f", "", "Path to the descriptor YAML or JSON file (required)")
	stackTemplateCreateCmd.Flags().String("name", "", "URL-safe slug for the template (required)")
	stackTemplateCreateCmd.Flags().String("display-name", "", "Human-readable title (defaults to --name)")
	stackTemplateCreateCmd.Flags().String("description", "", "Optional long-form description")
	stackTemplateCreateCmd.Flags().String("version", "1.0.0", "Semantic version string")
	stackTemplateCreateCmd.Flags().String("visibility", "private", "Visibility: private | org_shared | public")

	stackTemplateDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	stackTemplateCmd.AddCommand(stackTemplateCreateCmd)
	stackTemplateCmd.AddCommand(stackTemplateListCmd)
	stackTemplateCmd.AddCommand(stackTemplateMarketplaceCmd)
	stackTemplateCmd.AddCommand(stackTemplateGetCmd)
	stackTemplateCmd.AddCommand(stackTemplateDeleteCmd)
	stackTemplateCmd.AddCommand(stackTemplatePublishCmd)
	stackTemplateCmd.AddCommand(stackTemplateUnpublishCmd)
}

// ---------------------------------------------------------------------------
// Command implementations
// ---------------------------------------------------------------------------

func runStackTemplateCreate(cmd *cobra.Command, args []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	name, _ := cmd.Flags().GetString("name")
	displayName, _ := cmd.Flags().GetString("display-name")
	description, _ := cmd.Flags().GetString("description")
	version, _ := cmd.Flags().GetString("version")
	visibility, _ := cmd.Flags().GetString("visibility")

	if filePath == "" {
		return fmt.Errorf("-f / --file is required")
	}
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if displayName == "" {
		displayName = name
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read descriptor file: %w", err)
	}

	req := createStackTemplateRequest{
		Name:        name,
		DisplayName: displayName,
		Description: description,
		Version:     version,
		Visibility:  visibility,
		Descriptor:  string(raw),
	}

	fmt.Printf("Creating stack template %q...\n", name)
	var t customerStackTemplate
	if err := stackDoPost("/stacks/templates", req, &t); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(t)
	}

	fmt.Printf("Template created successfully.\n")
	fmt.Printf("  ID:         %s\n", t.ID)
	fmt.Printf("  Name:       %s\n", t.Name)
	fmt.Printf("  Version:    %s\n", t.Version)
	fmt.Printf("  Visibility: %s\n", t.Visibility)
	fmt.Printf("  Published:  %v\n", t.Published)
	if t.Visibility == "public" && !t.Published {
		fmt.Printf("\nRun 'fdb stack template publish %s' to make it visible in the marketplace.\n", t.ID)
	}
	return nil
}

func runStackTemplateList(cmd *cobra.Command, args []string) error {
	var resp listCustomerTemplatesResponse
	if err := stackDoGet("/stacks/templates/mine", &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	if len(resp.Templates) == 0 {
		fmt.Println("No templates found.")
		return nil
	}

	tbl := newStackTable([]string{"ID", "NAME", "VERSION", "VISIBILITY", "PUBLISHED"})
	for _, t := range resp.Templates {
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		published := "no"
		if t.Published {
			published = "yes"
		}
		tbl.Append([]string{shortID, t.Name, t.Version, t.Visibility, published})
	}
	tbl.Render()
	fmt.Printf("\nTotal: %d template(s)\n", len(resp.Templates))
	return nil
}

func runStackTemplateMarketplace(cmd *cobra.Command, args []string) error {
	var resp listCustomerTemplatesResponse
	if err := stackDoGet("/stacks/templates/marketplace", &resp); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(resp)
	}

	if len(resp.Templates) == 0 {
		fmt.Println("No marketplace templates available.")
		return nil
	}

	tbl := newStackTable([]string{"ID", "NAME", "DISPLAY NAME", "VERSION", "DESCRIPTION"})
	for _, t := range resp.Templates {
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		desc := t.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		tbl.Append([]string{shortID, t.Name, t.DisplayName, t.Version, desc})
	}
	tbl.Render()
	fmt.Printf("\nTotal: %d marketplace template(s)\n", len(resp.Templates))
	return nil
}

func runStackTemplateGet(cmd *cobra.Command, args []string) error {
	var t customerStackTemplate
	if err := stackDoGet("/stacks/templates/"+args[0], &t); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(t)
	}

	fmt.Printf("ID:           %s\n", t.ID)
	fmt.Printf("Name:         %s\n", t.Name)
	fmt.Printf("Display name: %s\n", t.DisplayName)
	if t.Description != "" {
		fmt.Printf("Description:  %s\n", t.Description)
	}
	fmt.Printf("Version:      %s\n", t.Version)
	fmt.Printf("Visibility:   %s\n", t.Visibility)
	published := "no"
	if t.Published {
		published = "yes"
	}
	fmt.Printf("Published:    %s\n", published)
	if t.OrganizationID != "" {
		fmt.Printf("Organization: %s\n", t.OrganizationID)
	}
	fmt.Printf("Created:      %s\n", t.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("Updated:      %s\n", t.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))
	return nil
}

func runStackTemplateDelete(cmd *cobra.Command, args []string) error {
	autoYes, _ := cmd.Flags().GetBool("yes")
	templateID := args[0]

	if !autoYes {
		fmt.Printf("This will permanently delete template %q.\n", templateID)
		fmt.Print("Confirm? [y/N]: ")

		var answer string
		fmt.Scanln(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Delete cancelled.")
			return nil
		}
	}

	if err := stackDoDelete("/stacks/templates/" + templateID); err != nil {
		return err
	}

	fmt.Printf("Template %q deleted.\n", templateID)
	return nil
}

func runStackTemplatePublish(cmd *cobra.Command, args []string) error {
	templateID := args[0]
	fmt.Printf("Publishing template %q to the marketplace...\n", templateID)

	var t customerStackTemplate
	if err := stackDoPost("/stacks/templates/"+templateID+"/publish", nil, &t); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(t)
	}

	fmt.Printf("Template %q is now published in the marketplace.\n", t.ID)
	fmt.Printf("  Name:       %s\n", t.Name)
	fmt.Printf("  Version:    %s\n", t.Version)
	fmt.Printf("  Visibility: %s\n", t.Visibility)
	return nil
}

func runStackTemplateUnpublish(cmd *cobra.Command, args []string) error {
	templateID := args[0]
	fmt.Printf("Unpublishing template %q from the marketplace...\n", templateID)

	var t customerStackTemplate
	if err := stackDoPost("/stacks/templates/"+templateID+"/unpublish", nil, &t); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(t)
	}

	fmt.Printf("Template %q removed from the marketplace (not deleted).\n", t.ID)
	return nil
}

// ---------------------------------------------------------------------------
// Shared HTTP helpers for stack Phase 2 commands.
// ---------------------------------------------------------------------------

func stackGetAPIParams() (baseURL, user, pass, org string) {
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

func stackDoRequest(method, path string, body interface{}) ([]byte, error) {
	baseURL, user, pass, org := stackGetAPIParams()

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

func stackDoGet(path string, dest interface{}) error {
	data, err := stackDoRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func stackDoPost(path string, body, dest interface{}) error {
	data, err := stackDoRequest(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	if dest != nil {
		if err := json.Unmarshal(data, dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func stackDoDelete(path string) error {
	_, err := stackDoRequest(http.MethodDelete, path, nil)
	return err
}

// newStackTable returns a pre-configured tablewriter with the standard stack style.
func newStackTable(headers []string) *tablewriter.Table {
	tbl := tablewriter.NewWriter(os.Stdout)
	tbl.SetHeader(headers)
	tbl.SetBorder(false)
	tbl.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	tbl.SetAlignment(tablewriter.ALIGN_LEFT)
	tbl.SetCenterSeparator("")
	tbl.SetColumnSeparator("  ")
	tbl.SetRowSeparator("")
	tbl.SetHeaderLine(false)
	tbl.SetTablePadding("  ")
	tbl.SetNoWhiteSpace(true)
	return tbl
}
