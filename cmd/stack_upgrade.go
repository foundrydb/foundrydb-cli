package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Local types for the stack upgrade API surface.
// ---------------------------------------------------------------------------

type stackUpgradeChangeItem struct {
	Resource string `json:"resource"`
	Field    string `json:"field"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

type stackUpgradePreview struct {
	StackID                string                   `json:"stack_id"`
	TemplateName           string                   `json:"template_name"`
	CurrentTemplateVersion string                   `json:"current_template_version"`
	NewTemplateVersion     string                   `json:"new_template_version"`
	Changes                []stackUpgradeChangeItem `json:"changes"`
	CurrentMonthlyCost     float64                  `json:"current_monthly_cost"`
	NewMonthlyCost         float64                  `json:"new_monthly_cost"`
	CostDelta              float64                  `json:"cost_delta"`
	Currency               string                   `json:"currency"`
}

// ---------------------------------------------------------------------------
// Command tree: fdb stack upgrade
// ---------------------------------------------------------------------------

var stackUpgradeCmd = &cobra.Command{
	Use:   "upgrade <stack-id>",
	Short: "Upgrade a stack to the latest version of its template",
	Long: `Preview and apply an upgrade for a running stack.

The command fetches the change list and cost delta for the latest version of
the stack's template, displays a summary, then prompts for confirmation before
applying. Use --yes to skip the confirmation.

Example:
  fdb stack upgrade abc123
  fdb stack upgrade abc123 --yes`,
	Args: cobra.ExactArgs(1),
	RunE: runStackUpgrade,
}

var stackUpgradePreviewCmd = &cobra.Command{
	Use:   "preview <stack-id>",
	Short: "Preview the changes and cost delta for an available stack upgrade",
	Args:  cobra.ExactArgs(1),
	RunE:  runStackUpgradePreview,
}

func init() {
	stackUpgradeCmd.Flags().Bool("yes", false, "Apply the upgrade without an interactive confirmation prompt")

	// stackUpgradeCmd is the apply command; stackUpgradePreviewCmd is a child
	// so "fdb stack upgrade preview <id>" works, but "fdb stack upgrade <id>"
	// also works directly.
	stackUpgradeCmd.AddCommand(stackUpgradePreviewCmd)
}

// ---------------------------------------------------------------------------
// Command implementations
// ---------------------------------------------------------------------------

func runStackUpgradePreview(cmd *cobra.Command, args []string) error {
	stackID := args[0]

	var preview stackUpgradePreview
	if err := stackDoGet("/stacks/"+stackID+"/upgrade/preview", &preview); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(preview)
	}

	printUpgradePreview(&preview)
	return nil
}

func runStackUpgrade(cmd *cobra.Command, args []string) error {
	stackID := args[0]
	autoYes, _ := cmd.Flags().GetBool("yes")

	fmt.Printf("Fetching upgrade preview for stack %q...\n", stackID)
	var preview stackUpgradePreview
	if err := stackDoGet("/stacks/"+stackID+"/upgrade/preview", &preview); err != nil {
		return err
	}

	printUpgradePreview(&preview)

	if !autoYes {
		sign := "+"
		if preview.CostDelta < 0 {
			sign = ""
		}
		fmt.Printf("\nCost change: %s$%.2f/mo\n", sign, preview.CostDelta)
		fmt.Printf("New monthly cost: $%.2f/mo\n", preview.NewMonthlyCost)
		fmt.Print("Apply upgrade? [y/N]: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer != "y" && answer != "yes" {
				fmt.Println("Upgrade cancelled.")
				return nil
			}
		} else {
			return fmt.Errorf("no input received; upgrade cancelled")
		}
	}

	type applyReq struct {
		AcceptedMonthlyCost float64 `json:"accepted_monthly_cost"`
	}
	body := applyReq{AcceptedMonthlyCost: preview.NewMonthlyCost}

	fmt.Printf("Applying upgrade for stack %q...\n", stackID)

	// We post to /stacks/<id>/upgrade and decode the returned stack object.
	type upgradeStack struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	var stack upgradeStack
	if err := stackDoPost("/stacks/"+stackID+"/upgrade", body, &stack); err != nil {
		return err
	}

	if jsonOut {
		return printJSON(stack)
	}

	fmt.Printf("Upgrade initiated successfully.\n")
	fmt.Printf("  ID:     %s\n", stack.ID)
	fmt.Printf("  Name:   %s\n", stack.Name)
	fmt.Printf("  Status: %s\n", stack.Status)
	fmt.Printf("\nUse 'fdb stack get %s' to monitor progress.\n", stack.ID)
	return nil
}

func printUpgradePreview(preview *stackUpgradePreview) {
	fmt.Printf("\nUpgrade preview for stack %q\n", preview.StackID)
	fmt.Printf("  Template:         %s\n", preview.TemplateName)
	fmt.Printf("  Current version:  %s\n", preview.CurrentTemplateVersion)
	fmt.Printf("  New version:      %s\n", preview.NewTemplateVersion)

	if len(preview.Changes) > 0 {
		fmt.Printf("\nChanges:\n")
		tbl := newStackTable([]string{"RESOURCE", "FIELD", "CURRENT VALUE", "NEW VALUE"})
		for _, c := range preview.Changes {
			tbl.Append([]string{c.Resource, c.Field, c.OldValue, c.NewValue})
		}
		tbl.Render()
	} else {
		fmt.Printf("\nNo field changes; this upgrade may refresh images or config only.\n")
	}

	sign := "+"
	if preview.CostDelta < 0 {
		sign = ""
	}
	fmt.Printf("\nCurrent monthly cost: $%.2f/mo\n", preview.CurrentMonthlyCost)
	fmt.Printf("New monthly cost:      $%.2f/mo\n", preview.NewMonthlyCost)
	fmt.Printf("Cost delta:           %s$%.2f/mo\n", sign, preview.CostDelta)
}
