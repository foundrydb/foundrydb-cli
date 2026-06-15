package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var jobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Manage jobs on app services",
}

var jobsListCmd = &cobra.Command{
	Use:   "list <app-service-id>",
	Short: "List jobs on an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runJobsList,
}

var jobsGetCmd = &cobra.Command{
	Use:   "get <app-service-id> <job-id>",
	Short: "Get details of a job",
	Args:  cobra.ExactArgs(2),
	RunE:  runJobsGet,
}

var jobsCreateCmd = &cobra.Command{
	Use:   "create <app-service-id>",
	Short: "Create a new job on an app service",
	Args:  cobra.ExactArgs(1),
	RunE:  runJobsCreate,
}

var jobsUpdateCmd = &cobra.Command{
	Use:   "update <app-service-id> <job-id>",
	Short: "Update a job definition",
	Args:  cobra.ExactArgs(2),
	RunE:  runJobsUpdate,
}

var jobsDeleteCmd = &cobra.Command{
	Use:   "delete <app-service-id> <job-id>",
	Short: "Delete a job definition",
	Args:  cobra.ExactArgs(2),
	RunE:  runJobsDelete,
}

var jobsRunCmd = &cobra.Command{
	Use:   "run <app-service-id> <job-id>",
	Short: "Trigger a manual invocation of a job",
	Args:  cobra.ExactArgs(2),
	RunE:  runJobsRun,
}

var jobsInvocationsCmd = &cobra.Command{
	Use:   "invocations <app-service-id> <job-id>",
	Short: "List invocation history of a job",
	Args:  cobra.ExactArgs(2),
	RunE:  runJobsInvocations,
}

var jobsLogsCmd = &cobra.Command{
	Use:   "logs <app-service-id> <job-id> <invocation-id>",
	Short: "Retrieve logs for a job invocation",
	Args:  cobra.ExactArgs(3),
	RunE:  runJobsLogs,
}

func init() {
	jobsCreateCmd.Flags().String("name", "", "Job name (required)")
	jobsCreateCmd.Flags().String("schedule", "", "Cron expression (e.g. '0 * * * *', @daily); omit for manual-only jobs")
	jobsCreateCmd.Flags().String("timezone", "UTC", "Timezone for schedule evaluation (e.g. America/New_York)")
	jobsCreateCmd.Flags().String("image", "", "Container image override (inherits app image when empty)")
	jobsCreateCmd.Flags().StringArray("command", []string{}, "Container argv override (exec form, repeatable)")
	jobsCreateCmd.Flags().StringArray("env", []string{}, "Environment variable overrides as KEY=VALUE (repeatable)")
	jobsCreateCmd.Flags().Int("max-retries", 0, "Maximum number of retries on failure")
	jobsCreateCmd.Flags().Int("max-runtime", 3600, "Maximum runtime in seconds (default 3600)")
	jobsCreateCmd.Flags().Int("concurrency-cap", 1, "Maximum concurrent invocations (default 1)")

	jobsUpdateCmd.Flags().String("schedule", "", "New cron expression")
	jobsUpdateCmd.Flags().Bool("clear-schedule", false, "Remove the schedule (make job manual-only)")
	jobsUpdateCmd.Flags().String("timezone", "", "New timezone")
	jobsUpdateCmd.Flags().String("image", "", "New container image override")
	jobsUpdateCmd.Flags().Bool("clear-image", false, "Remove the image override (inherit from app)")
	jobsUpdateCmd.Flags().StringArray("command", []string{}, "New container argv override (repeatable)")
	jobsUpdateCmd.Flags().StringArray("env", []string{}, "New environment variable overrides as KEY=VALUE (repeatable)")
	jobsUpdateCmd.Flags().Int("max-retries", -1, "Maximum number of retries (-1 leaves unchanged)")
	jobsUpdateCmd.Flags().Int("max-runtime", -1, "Maximum runtime in seconds (-1 leaves unchanged)")
	jobsUpdateCmd.Flags().Int("concurrency-cap", -1, "Maximum concurrent invocations (-1 leaves unchanged)")
	jobsUpdateCmd.Flags().Bool("enable", false, "Enable the job")
	jobsUpdateCmd.Flags().Bool("disable", false, "Disable the job")

	jobsDeleteCmd.Flags().Bool("confirm", false, "Skip confirmation prompt")

	jobsInvocationsCmd.Flags().Int("limit", 20, "Maximum number of invocations to show (default 20, max 200)")

	jobsLogsCmd.Flags().IntP("lines", "n", 200, "Number of log lines to retrieve")

	jobsCmd.AddCommand(jobsListCmd)
	jobsCmd.AddCommand(jobsGetCmd)
	jobsCmd.AddCommand(jobsCreateCmd)
	jobsCmd.AddCommand(jobsUpdateCmd)
	jobsCmd.AddCommand(jobsDeleteCmd)
	jobsCmd.AddCommand(jobsRunCmd)
	jobsCmd.AddCommand(jobsInvocationsCmd)
	jobsCmd.AddCommand(jobsLogsCmd)
}

func runJobsList(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	jobs, err := client.ListAppJobs(ctx, appID)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(jobs)
	}

	if len(jobs) == 0 {
		fmt.Printf("No jobs found on app service %q.\n", args[0])
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "NAME", "SCHEDULE", "ENABLED", "NEXT RUN", "LAST RUN"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, j := range jobs {
		shortID := j.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		schedule := "-"
		if j.ScheduleCron != nil {
			schedule = *j.ScheduleCron
		}
		enabled := "no"
		if j.Enabled {
			enabled = "yes"
		}
		nextRun := "-"
		if j.NextRunAt != nil {
			nextRun = j.NextRunAt.Format("2006-01-02 15:04")
		}
		lastRun := "-"
		if j.LastRunAt != nil {
			lastRun = j.LastRunAt.Format("2006-01-02 15:04")
		}
		table.Append([]string{shortID, j.Name, schedule, enabled, nextRun, lastRun})
	}
	table.Render()
	fmt.Printf("\nTotal: %d jobs\n", len(jobs))
	return nil
}

func runJobsGet(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	job, err := client.GetAppJob(ctx, appID, args[1])
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job %q not found on app service %q", args[1], args[0])
	}

	if jsonOut {
		return printJSON(job)
	}

	fmt.Printf("ID:              %s\n", job.ID)
	fmt.Printf("Name:            %s\n", job.Name)
	fmt.Printf("Enabled:         %v\n", job.Enabled)
	if job.ScheduleCron != nil {
		fmt.Printf("Schedule:        %s (%s)\n", *job.ScheduleCron, job.Timezone)
	} else {
		fmt.Printf("Schedule:        manual only\n")
	}
	if job.ImageRef != nil {
		fmt.Printf("Image override:  %s\n", *job.ImageRef)
	}
	if len(job.Command) > 0 {
		fmt.Printf("Command:         %s\n", strings.Join(job.Command, " "))
	}
	fmt.Printf("Max retries:     %d\n", job.MaxRetries)
	fmt.Printf("Max runtime:     %ds\n", job.MaxRuntimeSeconds)
	fmt.Printf("Concurrency cap: %d\n", job.ConcurrencyCap)
	if job.NextRunAt != nil {
		fmt.Printf("Next run:        %s\n", job.NextRunAt.Format(time.RFC3339))
	}
	if job.LastRunAt != nil {
		fmt.Printf("Last run:        %s\n", job.LastRunAt.Format(time.RFC3339))
	}
	fmt.Printf("Created:         %s\n", job.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:         %s\n", job.UpdatedAt.Format(time.RFC3339))
	return nil
}

func runJobsCreate(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		fmt.Print("Job name: ")
		fmt.Scanln(&name)
	}
	if name == "" {
		return fmt.Errorf("job name is required")
	}

	schedule, _ := cmd.Flags().GetString("schedule")
	timezone, _ := cmd.Flags().GetString("timezone")
	imageStr, _ := cmd.Flags().GetString("image")
	commandArr, _ := cmd.Flags().GetStringArray("command")
	envArr, _ := cmd.Flags().GetStringArray("env")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")
	maxRuntime, _ := cmd.Flags().GetInt("max-runtime")
	concurrencyCap, _ := cmd.Flags().GetInt("concurrency-cap")

	req := foundrydb.AppJobCreateRequest{
		Name:     name,
		Timezone: timezone,
	}

	if schedule != "" {
		req.ScheduleCron = &schedule
	}
	if imageStr != "" {
		req.ImageRef = &imageStr
	}
	if len(commandArr) > 0 {
		req.Command = commandArr
	}
	if len(envArr) > 0 {
		envMap, parseErr := parseEnvPairs(envArr)
		if parseErr != nil {
			return parseErr
		}
		req.Env = envMap
	}
	if maxRetries > 0 {
		req.MaxRetries = &maxRetries
	}
	if maxRuntime != 3600 {
		req.MaxRuntimeSeconds = &maxRuntime
	}
	if concurrencyCap != 1 {
		req.ConcurrencyCap = &concurrencyCap
	}

	job, err := client.CreateAppJob(ctx, appID, req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(job)
	}

	fmt.Printf("Job %q created (ID: %s).\n", job.Name, job.ID)
	if job.ScheduleCron != nil {
		fmt.Printf("Schedule: %s (%s)\n", *job.ScheduleCron, job.Timezone)
	} else {
		fmt.Printf("Schedule: manual only (use 'fdb jobs run %s %s' to trigger)\n", args[0], job.ID)
	}
	return nil
}

func runJobsUpdate(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	clearSchedule, _ := cmd.Flags().GetBool("clear-schedule")
	clearImage, _ := cmd.Flags().GetBool("clear-image")
	enableFlag, _ := cmd.Flags().GetBool("enable")
	disableFlag, _ := cmd.Flags().GetBool("disable")

	if enableFlag && disableFlag {
		return fmt.Errorf("--enable and --disable are mutually exclusive")
	}

	req := foundrydb.AppJobPatchRequest{
		ClearSchedule: clearSchedule,
		ClearImageRef: clearImage,
	}

	if schedule, _ := cmd.Flags().GetString("schedule"); schedule != "" {
		req.ScheduleCron = &schedule
	}
	if tz, _ := cmd.Flags().GetString("timezone"); tz != "" {
		req.Timezone = &tz
	}
	if imageStr, _ := cmd.Flags().GetString("image"); imageStr != "" {
		req.ImageRef = &imageStr
	}
	if commandArr, _ := cmd.Flags().GetStringArray("command"); len(commandArr) > 0 {
		req.Command = commandArr
	}
	if envArr, _ := cmd.Flags().GetStringArray("env"); len(envArr) > 0 {
		envMap, parseErr := parseEnvPairs(envArr)
		if parseErr != nil {
			return parseErr
		}
		req.Env = envMap
	}
	if maxRetries, _ := cmd.Flags().GetInt("max-retries"); maxRetries >= 0 {
		req.MaxRetries = &maxRetries
	}
	if maxRuntime, _ := cmd.Flags().GetInt("max-runtime"); maxRuntime >= 0 {
		req.MaxRuntimeSeconds = &maxRuntime
	}
	if concurrencyCap, _ := cmd.Flags().GetInt("concurrency-cap"); concurrencyCap >= 0 {
		req.ConcurrencyCap = &concurrencyCap
	}
	if enableFlag {
		t := true
		req.Enabled = &t
	} else if disableFlag {
		f := false
		req.Enabled = &f
	}

	job, err := client.UpdateAppJob(ctx, appID, args[1], req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(job)
	}

	fmt.Printf("Job %q updated.\n", job.Name)
	return nil
}

func runJobsDelete(cmd *cobra.Command, args []string) error {
	confirmed, _ := cmd.Flags().GetBool("confirm")
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	jobID := args[1]

	if !confirmed {
		job, getErr := client.GetAppJob(ctx, appID, jobID)
		jobName := jobID
		if getErr == nil && job != nil {
			jobName = job.Name
		}
		fmt.Printf("This will permanently delete job %q (ID: %s) and its invocation history.\n", jobName, jobID)
		fmt.Print("Type the job name to confirm: ")
		var input string
		fmt.Scanln(&input)
		if input != jobName {
			return fmt.Errorf("confirmation failed: expected %q, got %q", jobName, input)
		}
	}

	if err := client.DeleteAppJob(ctx, appID, jobID); err != nil {
		return err
	}

	fmt.Printf("Job %q deleted.\n", jobID)
	return nil
}

func runJobsRun(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	jobID := args[1]
	inv, err := client.RunAppJob(ctx, appID, jobID)
	if err != nil {
		// Detect 409 Conflict (concurrency cap) and surface a clear message.
		if apiErr, ok := err.(*foundrydb.APIError); ok && apiErr.StatusCode == 409 {
			return fmt.Errorf("job is already at its concurrency cap; retry once a running invocation finishes (use 'fdb jobs invocations %s %s' to check)", args[0], jobID)
		}
		return err
	}

	if jsonOut {
		return printJSON(inv)
	}

	fmt.Printf("Invocation queued (ID: %s, status: %s).\n", inv.ID, inv.Status)
	fmt.Printf("Use 'fdb jobs invocations %s %s' to monitor progress.\n", args[0], jobID)
	fmt.Printf("Use 'fdb jobs logs %s %s %s' to retrieve logs once complete.\n", args[0], jobID, inv.ID)
	return nil
}

func runJobsInvocations(cmd *cobra.Command, args []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	invocations, err := client.ListAppJobInvocations(ctx, appID, args[1], limit, 0)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(invocations)
	}

	if len(invocations) == 0 {
		fmt.Printf("No invocations found for job %q.\n", args[1])
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "STATUS", "ATTEMPT", "TRIGGER", "EXIT CODE", "DURATION", "FINISHED"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, inv := range invocations {
		shortID := inv.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		exitCode := "-"
		if inv.ExitCode != nil {
			exitCode = fmt.Sprintf("%d", *inv.ExitCode)
		}
		duration := "-"
		if inv.DurationMs != nil {
			dur := time.Duration(*inv.DurationMs) * time.Millisecond
			duration = formatDuration(dur)
		}
		finished := "-"
		if inv.FinishedAt != nil {
			finished = inv.FinishedAt.Format("2006-01-02 15:04:05")
		}
		table.Append([]string{
			shortID,
			inv.Status,
			fmt.Sprintf("%d", inv.Attempt),
			inv.TriggeredBy,
			exitCode,
			duration,
			finished,
		})
	}
	table.Render()
	fmt.Printf("\nShowing %d invocations (newest first)\n", len(invocations))
	return nil
}

func runJobsLogs(cmd *cobra.Command, args []string) error {
	lines, _ := cmd.Flags().GetInt("lines")
	client := newClient()
	ctx := context.Background()

	appID, err := resolveAppServiceID(client, args[0])
	if err != nil {
		return err
	}

	jobID := args[1]
	invocationID := args[2]

	fmt.Printf("Requesting logs for invocation %q...\n", invocationID)

	taskID, err := client.RequestAppJobInvocationLogs(ctx, appID, jobID, invocationID, lines)
	if err != nil {
		return fmt.Errorf("request logs: %w", err)
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		result, pollErr := client.GetAppJobInvocationLogs(ctx, appID, jobID, invocationID, taskID)
		if pollErr != nil {
			return fmt.Errorf("poll logs: %w", pollErr)
		}

		switch result.Status {
		case "COMPLETED":
			if jsonOut {
				return printJSON(result)
			}
			if result.Result != nil {
				for _, line := range result.Result.Lines {
					fmt.Println(line)
				}
				if result.Result.TruncatedAt != nil {
					fmt.Printf("\n(log truncated at line %d; use --lines to increase the limit)\n", *result.Result.TruncatedAt)
				}
			}
			return nil
		case "FAILED", "TIMEOUT", "CANCELLED":
			msg := result.ErrorMessage
			if msg == "" {
				msg = result.Status
			}
			return fmt.Errorf("log retrieval failed: %s", msg)
		default:
			// PENDING, DISPATCHED, IN_PROGRESS
			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("timed out waiting for log retrieval (task_id: %s)", taskID)
}

// resolveAppServiceID returns the UUID for the given ID-or-name. When the
// input is already a UUID (contains hyphens and is long), it is returned as-is
// without a round trip.
func resolveAppServiceID(client *foundrydb.Client, idOrName string) (string, error) {
	// Heuristic: UUIDs are 36 chars with hyphens.
	if len(idOrName) == 36 && strings.Count(idOrName, "-") == 4 {
		return idOrName, nil
	}
	app, err := resolveAppService(client, idOrName)
	if err != nil {
		return "", err
	}
	return app.ID, nil
}

// parseEnvPairs converts a slice of "KEY=VALUE" strings into a map.
func parseEnvPairs(pairs []string) (map[string]string, error) {
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		idx := strings.IndexByte(pair, '=')
		if idx < 1 {
			return nil, fmt.Errorf("invalid env pair %q: expected KEY=VALUE", pair)
		}
		result[pair[:idx]] = pair[idx+1:]
	}
	return result, nil
}

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", mins, secs)
}

// printJobJSON marshals a value to JSON and prints it.
func printJobJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
