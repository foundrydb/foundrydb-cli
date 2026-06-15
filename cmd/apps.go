package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "Manage app services",
}

var appsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all app services",
	RunE:  runAppsList,
}

var appsGetCmd = &cobra.Command{
	Use:   "get <id-or-name>",
	Short: "Get details of an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsGet,
}

var appsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new app service",
	RunE:  runAppsCreate,
}

var appsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsDelete,
}

var appsRestartCmd = &cobra.Command{
	Use:   "restart <id>",
	Short: "Restart an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsRestart,
}

var appsLogsCmd = &cobra.Command{
	Use:   "logs <id>",
	Short: "Retrieve logs from an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runAppsLogs,
}

func init() {
	appsCreateCmd.Flags().String("name", "", "App service name (required)")
	appsCreateCmd.Flags().String("image", "", "Container image reference (required)")
	appsCreateCmd.Flags().Int("port", 8080, "Container port")
	appsCreateCmd.Flags().StringToString("env", map[string]string{}, "Environment variables as KEY=VALUE")
	appsCreateCmd.Flags().String("plan", "tier-2", "Compute plan (e.g. tier-2)")
	appsCreateCmd.Flags().String("zone", "se-sto1", "Cloud zone (default: se-sto1)")
	appsCreateCmd.Flags().Int("storage-size", 20, "Storage size in GB")
	appsCreateCmd.Flags().String("storage-tier", "maxiops", "Storage tier: standard or maxiops")
	appsCreateCmd.Flags().StringSlice("attach", []string{}, "Service IDs to attach at creation")

	appsDeleteCmd.Flags().Bool("confirm", false, "Skip confirmation prompt")

	appsLogsCmd.Flags().IntP("lines", "n", 200, "Number of log lines to retrieve")

	appsCmd.AddCommand(appsListCmd)
	appsCmd.AddCommand(appsGetCmd)
	appsCmd.AddCommand(appsCreateCmd)
	appsCmd.AddCommand(appsDeleteCmd)
	appsCmd.AddCommand(appsRestartCmd)
	appsCmd.AddCommand(appsLogsCmd)
}

func runAppsList(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()
	apps, err := client.ListAppServices(ctx)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(apps)
	}

	if len(apps) == 0 {
		fmt.Println("No app services found.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "NAME", "STATUS", "PLAN", "ZONE", "IMAGE"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, app := range apps {
		shortID := app.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		imageRef := ""
		if app.AppConfig != nil {
			imageRef = app.AppConfig.ImageRef
			if len(imageRef) > 40 {
				imageRef = imageRef[:37] + "..."
			}
		}
		table.Append([]string{
			shortID,
			app.Name,
			app.Status,
			app.PlanName,
			app.Zone,
			imageRef,
		})
	}
	table.Render()
	fmt.Printf("\nTotal: %d app services\n", len(apps))
	return nil
}

func runAppsGet(cmd *cobra.Command, args []string) error {
	client := newClient()
	app, err := resolveAppService(client, args[0])
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(app)
	}

	fmt.Printf("ID:       %s\n", app.ID)
	fmt.Printf("Name:     %s\n", app.Name)
	fmt.Printf("Status:   %s\n", app.Status)
	fmt.Printf("Plan:     %s\n", app.PlanName)
	fmt.Printf("Zone:     %s\n", app.Zone)
	fmt.Printf("Storage:  %d GB (%s)\n", app.StorageSizeGB, app.StorageTier)
	fmt.Printf("Created:  %s\n", app.CreatedAt)
	fmt.Printf("Updated:  %s\n", app.UpdatedAt)

	if app.AppConfig != nil {
		fmt.Printf("\nContainer:\n")
		fmt.Printf("  Image:  %s\n", app.AppConfig.ImageRef)
		fmt.Printf("  Port:   %d\n", app.AppConfig.ContainerPort)
		if app.AppConfig.HealthCheckPath != "" {
			fmt.Printf("  Health: %s\n", app.AppConfig.HealthCheckPath)
		}
		if len(app.AppConfig.Env) > 0 {
			fmt.Printf("  Env keys: %s\n", strings.Join(envKeys(app.AppConfig.Env), ", "))
		}
		if len(app.AppConfig.CustomDomains) > 0 {
			fmt.Printf("  Domains: %s\n", strings.Join(app.AppConfig.CustomDomains, ", "))
		}
	}

	if len(app.AttachedServiceIDs) > 0 {
		fmt.Printf("\nAttached services:\n")
		for _, id := range app.AttachedServiceIDs {
			fmt.Printf("  %s\n", id)
		}
	}

	return nil
}

func runAppsCreate(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	image, _ := cmd.Flags().GetString("image")
	port, _ := cmd.Flags().GetInt("port")
	envVars, _ := cmd.Flags().GetStringToString("env")
	plan, _ := cmd.Flags().GetString("plan")
	zone, _ := cmd.Flags().GetString("zone")
	storageSize, _ := cmd.Flags().GetInt("storage-size")
	storageTier, _ := cmd.Flags().GetString("storage-tier")
	attachIDs, _ := cmd.Flags().GetStringSlice("attach")

	if name == "" {
		fmt.Print("App service name: ")
		fmt.Scanln(&name)
	}
	if name == "" {
		return fmt.Errorf("app service name is required")
	}

	if image == "" {
		fmt.Print("Container image: ")
		fmt.Scanln(&image)
	}
	if image == "" {
		return fmt.Errorf("container image is required")
	}

	req := foundrydb.CreateAppServiceRequest{
		Name:        name,
		PlanName:    plan,
		Zone:        zone,
		StorageSizeGB: storageSize,
		StorageTier: storageTier,
		AppConfig: foundrydb.AppContainerConfig{
			ImageRef:      image,
			ContainerPort: port,
		},
	}

	if len(envVars) > 0 {
		req.AppConfig.Env = envVars
	}
	if len(attachIDs) > 0 {
		req.AttachedServiceIDs = attachIDs
	}

	fmt.Printf("Creating app service %q (image=%s, plan=%s, zone=%s)...\n", name, image, plan, zone)

	ctx := context.Background()
	client := newClient()
	app, err := client.CreateAppService(ctx, req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(app)
	}

	fmt.Printf("App service created successfully.\n")
	fmt.Printf("  ID:     %s\n", app.ID)
	fmt.Printf("  Name:   %s\n", app.Name)
	fmt.Printf("  Status: %s\n", app.Status)
	fmt.Printf("\nUse 'fdb apps get %s' to monitor provisioning progress.\n", app.ID)
	return nil
}

func runAppsDelete(cmd *cobra.Command, args []string) error {
	confirmed, _ := cmd.Flags().GetBool("confirm")
	client := newClient()

	app, err := resolveAppService(client, args[0])
	if err != nil {
		return err
	}

	if !confirmed {
		fmt.Printf("This will permanently delete app service %q (ID: %s).\n", app.Name, app.ID)
		fmt.Print("Type the app service name to confirm: ")
		var input string
		fmt.Scanln(&input)
		if input != app.Name {
			return fmt.Errorf("confirmation failed: expected %q, got %q", app.Name, input)
		}
	}

	fmt.Printf("Deleting app service %q...\n", app.Name)
	ctx := context.Background()
	if err := client.DeleteAppService(ctx, app.ID); err != nil {
		return err
	}

	fmt.Printf("App service %q has been deleted.\n", app.Name)
	return nil
}

func runAppsRestart(cmd *cobra.Command, args []string) error {
	client := newClient()

	app, err := resolveAppService(client, args[0])
	if err != nil {
		return err
	}

	ctx := context.Background()
	fmt.Printf("Restarting app service %q...\n", app.Name)
	if err := client.RestartAppService(ctx, app.ID); err != nil {
		return err
	}

	fmt.Printf("App service %q restart initiated.\n", app.Name)
	return nil
}

func runAppsLogs(cmd *cobra.Command, args []string) error {
	lines, _ := cmd.Flags().GetInt("lines")
	client := newClient()

	app, err := resolveAppService(client, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("Requesting logs for app service %q (last %d lines)...\n", app.Name, lines)

	taskID, err := requestAppServiceLogs(app.ID, lines)
	if err != nil {
		return fmt.Errorf("request logs: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		logsResp, pollErr := pollAppServiceLogs(app.ID, taskID)
		if pollErr != nil {
			return fmt.Errorf("poll logs: %w", pollErr)
		}

		switch logsResp.Status {
		case "completed", "done", "success", "COMPLETED":
			if jsonOut {
				data, _ := json.Marshal(logsResp)
				fmt.Println(string(data))
				return nil
			}
			fmt.Println(logsResp.Logs)
			return nil
		case "failed", "error", "FAILED":
			return fmt.Errorf("log retrieval failed")
		default:
			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("timed out waiting for log retrieval (task_id: %s)", taskID)
}

// requestAppServiceLogs sends POST /app-services/{id}/logs?lines=N.
func requestAppServiceLogs(appServiceID string, lines int) (string, error) {
	baseURL := viper.GetString("api_url")
	user := viper.GetString("username")
	pass := viper.GetString("password")
	org := viper.GetString("org")
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

	path := fmt.Sprintf("%s/app-services/%s/logs?lines=%d", strings.TrimRight(baseURL, "/"), appServiceID, lines)
	req, err := http.NewRequest(http.MethodPost, path, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Accept", "application/json")
	if org != "" {
		req.Header.Set("X-Active-Org-ID", org)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	var result struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.TaskID == "" {
		return "", fmt.Errorf("response missing task_id")
	}
	return result.TaskID, nil
}

// pollAppServiceLogs sends GET /app-services/{id}/logs?task_id=X.
func pollAppServiceLogs(appServiceID, taskID string) (*logsResponse, error) {
	baseURL := viper.GetString("api_url")
	user := viper.GetString("username")
	pass := viper.GetString("password")
	org := viper.GetString("org")
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

	path := fmt.Sprintf("%s/app-services/%s/logs?task_id=%s", strings.TrimRight(baseURL, "/"), appServiceID, taskID)
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Accept", "application/json")
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
	var result logsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// resolveAppService finds an app service by ID or name.
func resolveAppService(client *foundrydb.Client, idOrName string) (*foundrydb.AppService, error) {
	ctx := context.Background()

	app, err := client.GetAppService(ctx, idOrName)
	if err == nil && app != nil {
		return app, nil
	}

	apps, listErr := client.ListAppServices(ctx)
	if listErr != nil {
		return nil, fmt.Errorf("app service not found by ID and could not list services: %w", listErr)
	}

	var matches []foundrydb.AppService
	for _, a := range apps {
		if a.Name == idOrName {
			matches = append(matches, a)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no app service found with ID or name %q", idOrName)
	}
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.ID
		}
		return nil, fmt.Errorf("multiple app services named %q found, use an ID instead: %s", idOrName, strings.Join(ids, ", "))
	}

	return &matches[0], nil
}

// envKeys returns the sorted list of environment variable key names.
func envKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	return keys
}
