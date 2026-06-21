package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage vertical starter stacks",
	Long: `Manage vertical starter stacks composed from platform primitives.

A stack bundles a database, object storage, an inference key, and a companion
app into a single deployable unit. Use "fdb stack templates" to browse the
catalog, "fdb stack preview" to see the cost breakdown before you commit, and
"fdb stack launch" to provision everything in one step.`,
}

var stackTemplatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "List available stack templates",
	RunE:  runStackTemplates,
}

var stackPreviewCmd = &cobra.Command{
	Use:   "preview <template>",
	Short: "Preview the per-month cost breakdown for a stack template",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackPreview,
}

var stackLaunchCmd = &cobra.Command{
	Use:   "launch <template>",
	Short: "Launch a stack from a template",
	Long: `Launch a stack from a catalog template.

The command previews the monthly cost, then prompts for confirmation unless
--yes is given. The previewed cost is passed to the API as the accepted
monthly cost gate; the launch is rejected if prices change by more than $0.01
between the preview and the provisioning call.`,
	Args: cobra.ExactArgs(1),
	RunE: runStackLaunch,
}

var stackListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all launched stacks",
	RunE:  runStackList,
}

var stackGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get details of a stack including per-resource status",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackGet,
}

var stackDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Atomically tear down a stack and all its resources",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackDelete,
}

var stackRetryCmd = &cobra.Command{
	Use:   "retry <id>",
	Short: "Re-run provisioning for a failed stack",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackRetry,
}

func init() {
	stackLaunchCmd.Flags().String("name", "", "Name for the launched stack (required)")
	stackLaunchCmd.Flags().String("org", "", "Organization ID to scope the stack to")
	stackLaunchCmd.Flags().Bool("yes", false, "Accept the previewed cost automatically without an interactive prompt")

	stackDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	stackCmd.AddCommand(stackTemplatesCmd)
	stackCmd.AddCommand(stackPreviewCmd)
	stackCmd.AddCommand(stackLaunchCmd)
	stackCmd.AddCommand(stackListCmd)
	stackCmd.AddCommand(stackGetCmd)
	stackCmd.AddCommand(stackDeleteCmd)
	stackCmd.AddCommand(stackRetryCmd)
}

func runStackTemplates(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	templates, err := client.ListStackTemplates(ctx)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(templates)
	}

	if len(templates) == 0 {
		fmt.Println("No stack templates available.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"NAME", "DISPLAY NAME", "VERSION", "EST. MONTHLY COST"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, t := range templates {
		cost := "-"
		if t.CostPreview != nil {
			cost = fmt.Sprintf("$%.2f/mo", t.CostPreview.MonthlyTotal)
		}
		table.Append([]string{t.Name, t.DisplayName, t.Version, cost})
	}
	table.Render()
	fmt.Printf("\nTotal: %d templates\n", len(templates))
	return nil
}

func runStackPreview(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	preview, err := client.PreviewStackCost(ctx, foundrydb.StackPreviewRequest{
		TemplateName: args[0],
	})
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(preview)
	}

	printCostPreview(preview)
	return nil
}

func runStackLaunch(cmd *cobra.Command, args []string) error {
	templateName := args[0]

	name, _ := cmd.Flags().GetString("name")
	orgID, _ := cmd.Flags().GetString("org")
	autoYes, _ := cmd.Flags().GetBool("yes")

	if name == "" {
		fmt.Print("Stack name: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			name = strings.TrimSpace(scanner.Text())
		}
	}
	if name == "" {
		return fmt.Errorf("stack name is required (use --name)")
	}

	client := newClient()
	ctx := context.Background()

	fmt.Printf("Fetching cost preview for template %q...\n", templateName)
	preview, err := client.PreviewStackCost(ctx, foundrydb.StackPreviewRequest{
		TemplateName: templateName,
	})
	if err != nil {
		return fmt.Errorf("preview cost: %w", err)
	}

	printCostPreview(preview)

	if !autoYes {
		fmt.Printf("\nEstimated monthly cost: $%.2f/mo\n", preview.MonthlyTotal)
		fmt.Print("Accept and launch? [y/N]: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer != "y" && answer != "yes" {
				fmt.Println("Launch cancelled.")
				return nil
			}
		} else {
			return fmt.Errorf("no input received; launch cancelled")
		}
	}

	req := foundrydb.StackLaunchRequest{
		Name:                name,
		TemplateName:        templateName,
		AcceptedMonthlyCost: &preview.MonthlyTotal,
	}
	if orgID != "" {
		req.OrganizationID = orgID
	}

	fmt.Printf("Launching stack %q from template %q...\n", name, templateName)
	stack, err := client.LaunchStack(ctx, req)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(stack)
	}

	fmt.Printf("Stack launched successfully.\n")
	fmt.Printf("  ID:       %s\n", stack.ID)
	fmt.Printf("  Name:     %s\n", stack.Name)
	fmt.Printf("  Template: %s\n", stack.TemplateName)
	fmt.Printf("  Status:   %s\n", stack.Status)
	fmt.Printf("\nUse 'fdb stack get %s' to monitor provisioning progress.\n", stack.ID)
	return nil
}

func runStackList(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	stacks, err := client.ListStacks(ctx)
	if err != nil {
		return err
	}

	if jsonOut {
		return printJSON(stacks)
	}

	if len(stacks) == 0 {
		fmt.Println("No stacks found.")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "NAME", "TEMPLATE", "STATUS", "ENDPOINT"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, s := range stacks {
		shortID := s.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		endpoint := s.EndpointURL
		if endpoint == "" {
			endpoint = "-"
		}
		if len(endpoint) > 40 {
			endpoint = endpoint[:37] + "..."
		}
		table.Append([]string{shortID, s.Name, s.TemplateName, s.Status, endpoint})
	}
	table.Render()
	fmt.Printf("\nTotal: %d stacks\n", len(stacks))
	return nil
}

func runStackGet(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	stack, err := client.GetStack(ctx, args[0])
	if err != nil {
		return err
	}
	if stack == nil {
		return fmt.Errorf("stack %q not found", args[0])
	}

	if jsonOut {
		return printJSON(stack)
	}

	fmt.Printf("ID:               %s\n", stack.ID)
	fmt.Printf("Name:             %s\n", stack.Name)
	fmt.Printf("Template:         %s @ %s\n", stack.TemplateName, stack.TemplateVersion)
	fmt.Printf("Status:           %s\n", stack.Status)
	if stack.StatusDetail != "" {
		fmt.Printf("Status detail:    %s\n", stack.StatusDetail)
	}
	if stack.EndpointURL != "" {
		fmt.Printf("Endpoint:         %s\n", stack.EndpointURL)
	}
	fmt.Printf("Monthly cost:     $%.2f/mo\n", stack.EstimatedMonthlyCost)
	fmt.Printf("Created:          %s\n", stack.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("Updated:          %s\n", stack.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))

	if len(stack.Resources) > 0 {
		fmt.Printf("\nResources:\n")
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"SYMBOLIC NAME", "KIND", "STATUS", "SERVICE ID"})
		table.SetBorder(false)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
		table.SetAlignment(tablewriter.ALIGN_LEFT)
		table.SetCenterSeparator("")
		table.SetColumnSeparator("  ")
		table.SetRowSeparator("")
		table.SetHeaderLine(false)
		table.SetTablePadding("  ")
		table.SetNoWhiteSpace(true)

		for _, r := range stack.Resources {
			svcID := r.ServiceID
			if svcID == "" {
				svcID = r.RefID
			}
			if svcID == "" {
				svcID = "-"
			}
			if len(svcID) > 8 && strings.Contains(svcID, "-") {
				svcID = svcID[:8]
			}
			detail := r.Status
			if r.StatusDetail != "" {
				detail = r.Status + " (" + r.StatusDetail + ")"
			}
			table.Append([]string{r.SymbolicName, r.Kind, detail, svcID})
		}
		table.Render()
	}

	return nil
}

func runStackDelete(cmd *cobra.Command, args []string) error {
	autoYes, _ := cmd.Flags().GetBool("yes")
	client := newClient()
	ctx := context.Background()

	stackID := args[0]

	if !autoYes {
		stack, getErr := client.GetStack(ctx, stackID)
		stackName := stackID
		if getErr == nil && stack != nil {
			stackName = stack.Name
		}
		fmt.Printf("This will permanently delete stack %q (ID: %s) and all its resources.\n", stackName, stackID)
		fmt.Print("Type the stack name to confirm: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if input != stackName {
				return fmt.Errorf("confirmation failed: expected %q, got %q", stackName, input)
			}
		} else {
			return fmt.Errorf("no input received; delete cancelled")
		}
	}

	fmt.Printf("Deleting stack %q...\n", stackID)
	if err := client.DeleteStack(ctx, stackID); err != nil {
		return err
	}

	fmt.Printf("Stack %q teardown initiated.\n", stackID)
	return nil
}

func runStackRetry(cmd *cobra.Command, args []string) error {
	client := newClient()
	ctx := context.Background()

	stackID := args[0]
	fmt.Printf("Retrying stack %q...\n", stackID)
	if err := client.RetryStack(ctx, stackID); err != nil {
		return err
	}

	fmt.Printf("Stack %q re-queued for provisioning.\n", stackID)
	fmt.Printf("Use 'fdb stack get %s' to monitor progress.\n", stackID)
	return nil
}

// printCostPreview prints a formatted cost breakdown table.
func printCostPreview(preview *foundrydb.StackCostPreview) {
	fmt.Printf("\nCost preview for template %q (%s)\n\n", preview.TemplateName, preview.Currency)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"RESOURCE", "KIND", "DESCRIPTION", "MONTHLY COST"})
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("  ")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("  ")
	table.SetNoWhiteSpace(true)

	for _, item := range preview.LineItems {
		costStr := fmt.Sprintf("$%.2f", item.MonthlyCost)
		if item.IsCeiling {
			costStr += " (max)"
		}
		table.Append([]string{item.SymbolicName, item.Kind, item.Description, costStr})
	}
	table.Render()

	fmt.Printf("\nTotal: $%.2f/mo\n", preview.MonthlyTotal)

	if len(preview.Warnings) > 0 {
		fmt.Printf("\nNotes:\n")
		for _, w := range preview.Warnings {
			fmt.Printf("  * %s\n", w)
		}
	}
}
