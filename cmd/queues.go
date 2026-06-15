package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var queuesCmd = &cobra.Command{
	Use:   "queues",
	Short: "Manage message queues on managed services",
}

var queuesListCmd = &cobra.Command{
	Use:   "list <service-id>",
	Short: "List queues on a managed service",
	Args:  cobra.ExactArgs(1),
	RunE:  runQueuesList,
}

var queuesGetCmd = &cobra.Command{
	Use:   "get <service-id> <queue-name>",
	Short: "Get details of a queue",
	Args:  cobra.ExactArgs(2),
	RunE:  runQueuesGet,
}

var queuesCreateCmd = &cobra.Command{
	Use:   "create <service-id>",
	Short: "Create a new queue on a managed service",
	Args:  cobra.ExactArgs(1),
	RunE:  runQueuesCreate,
}

var queuesDeleteCmd = &cobra.Command{
	Use:   "delete <service-id> <queue-name>",
	Short: "Delete a queue and all its messages",
	Args:  cobra.ExactArgs(2),
	RunE:  runQueuesDelete,
}

var queuesEnqueueCmd = &cobra.Command{
	Use:   "enqueue <service-id> <queue-name>",
	Short: "Enqueue one or more messages onto a queue",
	Args:  cobra.ExactArgs(2),
	RunE:  runQueuesEnqueue,
}

var queuesStatsCmd = &cobra.Command{
	Use:   "stats <service-id> <queue-name>",
	Short: "Show depth statistics for a queue",
	Args:  cobra.ExactArgs(2),
	RunE:  runQueuesStats,
}

func init() {
	queuesCreateCmd.Flags().String("name", "", "Queue name (required)")
	queuesCreateCmd.Flags().Int("visibility-timeout", 0, "Redelivery horizon in seconds (default 30)")
	queuesCreateCmd.Flags().Int("max-attempts", 0, "Maximum delivery attempts before dead-lettering (default 5)")
	queuesCreateCmd.Flags().Bool("dlq", true, "Enable dead-letter queue (default true)")

	queuesDeleteCmd.Flags().Bool("confirm", false, "Skip confirmation prompt")

	queuesEnqueueCmd.Flags().StringArray("payload", []string{}, "JSON payload for a message (repeatable; each flag value is one message)")

	queuesCmd.AddCommand(queuesListCmd)
	queuesCmd.AddCommand(queuesGetCmd)
	queuesCmd.AddCommand(queuesCreateCmd)
	queuesCmd.AddCommand(queuesDeleteCmd)
	queuesCmd.AddCommand(queuesEnqueueCmd)
	queuesCmd.AddCommand(queuesStatsCmd)
}

func runQueuesList(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	svc, err := resolveService(client, args[0])
	if err != nil {
		return err
	}

	queues, err := client.ListQueues(ctx, svc.ID)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(queues)
	}

	if len(queues) == 0 {
		fmt.Printf("No queues found on service %q.\n", svc.Name)
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"NAME", "STATUS", "DB", "VIS TIMEOUT", "MAX ATTEMPTS", "DLQ"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, q := range queues {
		dlq := "no"
		if q.DLQEnabled {
			dlq = "yes"
		}
		table.Append([]string{
			q.Name,
			q.Status,
			q.DatabaseName,
			fmt.Sprintf("%ds", q.VisibilityTimeoutSeconds),
			fmt.Sprintf("%d", q.MaxAttempts),
			dlq,
		})
	}
	table.Render()
	fmt.Printf("\nTotal: %d queues\n", len(queues))
	return nil
}

func runQueuesGet(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	svc, err := resolveService(client, args[0])
	if err != nil {
		return err
	}

	q, err := client.GetQueue(ctx, svc.ID, args[1])
	if err != nil {
		return err
	}
	if q == nil {
		return fmt.Errorf("queue %q not found on service %q", args[1], svc.Name)
	}

	if jsonOut {
		return printJSON(q)
	}

	fmt.Printf("ID:                 %s\n", q.ID)
	fmt.Printf("Name:               %s\n", q.Name)
	fmt.Printf("Status:             %s\n", q.Status)
	fmt.Printf("Database:           %s\n", q.DatabaseName)
	fmt.Printf("Visibility timeout: %ds\n", q.VisibilityTimeoutSeconds)
	fmt.Printf("Max attempts:       %d\n", q.MaxAttempts)
	fmt.Printf("DLQ enabled:        %v\n", q.DLQEnabled)
	fmt.Printf("Created:            %s\n", q.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:            %s\n", q.UpdatedAt.Format(time.RFC3339))
	if q.ErrorMessage != nil {
		fmt.Printf("Error:              %s\n", *q.ErrorMessage)
	}
	return nil
}

func runQueuesCreate(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	svc, err := resolveService(client, args[0])
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		fmt.Print("Queue name: ")
		fmt.Scanln(&name)
	}
	if name == "" {
		return fmt.Errorf("queue name is required")
	}

	visTimeout, _ := cmd.Flags().GetInt("visibility-timeout")
	maxAttempts, _ := cmd.Flags().GetInt("max-attempts")
	dlqEnabled, _ := cmd.Flags().GetBool("dlq")

	req := foundrydb.QueueCreateRequest{
		Name: name,
	}
	if visTimeout > 0 {
		req.VisibilityTimeoutSeconds = &visTimeout
	}
	if maxAttempts > 0 {
		req.MaxAttempts = &maxAttempts
	}
	req.DLQEnabled = &dlqEnabled

	q, err := client.CreateQueue(ctx, svc.ID, req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(q)
	}

	fmt.Printf("Queue %q created on service %q (status: %s).\n", q.Name, svc.Name, q.Status)
	if q.Status != "Active" {
		fmt.Printf("Provisioning is asynchronous; use 'fdb queues get %s %s' to check when it reaches Active.\n", args[0], q.Name)
	}
	return nil
}

func runQueuesDelete(cmd *cobra.Command, args []string) error {
	confirmed, _ := cmd.Flags().GetBool("confirm")
	client := newClient()
	ctx := context.Background()

	svc, err := resolveService(client, args[0])
	if err != nil {
		return err
	}

	queueName := args[1]

	if !confirmed {
		fmt.Printf("This will permanently delete queue %q and all its pending messages.\n", queueName)
		fmt.Print("Type the queue name to confirm: ")
		var input string
		fmt.Scanln(&input)
		if input != queueName {
			return fmt.Errorf("confirmation failed: expected %q, got %q", queueName, input)
		}
	}

	q, err := client.DeleteQueue(ctx, svc.ID, queueName)
	if err != nil {
		return err
	}

	if q == nil {
		fmt.Printf("Queue %q not found (already deleted).\n", queueName)
		return nil
	}

	fmt.Printf("Queue %q is being deprovisioned (status: %s).\n", q.Name, q.Status)
	return nil
}

func runQueuesEnqueue(cmd *cobra.Command, args []string) error {
	payloadStrs, _ := cmd.Flags().GetStringArray("payload")
	if len(payloadStrs) == 0 {
		return fmt.Errorf("at least one --payload is required")
	}

	client := newClient()
	ctx := context.Background()

	svc, err := resolveService(client, args[0])
	if err != nil {
		return err
	}

	queueName := args[1]

	messages := make([]foundrydb.QueueEnqueueMessage, 0, len(payloadStrs))
	for i, raw := range payloadStrs {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return fmt.Errorf("--payload[%d] is not valid JSON: %w", i, err)
		}
		messages = append(messages, foundrydb.QueueEnqueueMessage{Payload: payload})
	}

	req := foundrydb.QueueEnqueueRequest{Messages: messages}

	fmt.Printf("Enqueuing %d message(s) onto queue %q...\n", len(messages), queueName)

	taskID, err := client.EnqueueQueueMessages(ctx, svc.ID, queueName, req)
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		result, pollErr := client.GetEnqueueResult(ctx, svc.ID, queueName, taskID)
		if pollErr != nil {
			return fmt.Errorf("poll enqueue: %w", pollErr)
		}

		switch result.Status {
		case "COMPLETED":
			if jsonOut {
				return printJSON(result)
			}
			if result.Result != nil {
				fmt.Printf("Enqueued %d message(s).\n", len(result.Result.MessageIDs))
				for i, id := range result.Result.MessageIDs {
					fmt.Printf("  [%d] message ID: %d\n", i+1, id)
				}
			}
			return nil
		case "FAILED", "TIMEOUT", "CANCELLED":
			return fmt.Errorf("enqueue failed (task %s status: %s)", taskID, result.Status)
		default:
			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("timed out waiting for enqueue to complete (task_id: %s)", taskID)
}

func runQueuesStats(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	svc, err := resolveService(client, args[0])
	if err != nil {
		return err
	}

	queueName := args[1]

	fmt.Printf("Requesting stats for queue %q...\n", queueName)

	taskID, err := client.RequestQueueStats(ctx, svc.ID, queueName)
	if err != nil {
		return fmt.Errorf("request stats: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		result, pollErr := client.GetQueueStats(ctx, svc.ID, queueName, taskID)
		if pollErr != nil {
			return fmt.Errorf("poll stats: %w", pollErr)
		}

		switch result.Status {
		case "COMPLETED":
			if jsonOut {
				return printJSON(result)
			}
			if result.Result != nil {
				printQueueStats(result.Result)
			}
			return nil
		case "FAILED", "TIMEOUT", "CANCELLED":
			return fmt.Errorf("stats request failed (task %s status: %s)", taskID, result.Status)
		default:
			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("timed out waiting for stats (task_id: %s)", taskID)
}

// printQueueStats renders a QueueStats snapshot as a table.
func printQueueStats(stats *foundrydb.QueueStats) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"QUEUE", "READY", "IN-FLIGHT", "DEAD", "OLDEST AGE"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	oldestAge := "-"
	if stats.OldestAgeSeconds > 0 {
		d := time.Duration(stats.OldestAgeSeconds * float64(time.Second))
		oldestAge = formatDuration(d)
	}

	table.Append([]string{
		stats.QueueName,
		fmt.Sprintf("%d", stats.ReadyMessages),
		fmt.Sprintf("%d", stats.InflightMessages),
		fmt.Sprintf("%d", stats.DeadMessages),
		oldestAge,
	})
	table.Render()
}
