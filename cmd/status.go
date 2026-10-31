package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/javaBin/javabin-cli/internal/aws"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var teamFlag string
var serviceFlag string
var resourcesFlag bool
var periodFlag string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show team costs and ECS service status",
	Long: `Show month-to-date AWS costs for a team and ECS service status.

Flags --team and --service override auto-detection. If run from a directory
with an app.yaml, team and service name are read from it automatically.

Use --resources to show resource-level cost breakdown from CUR data via Athena.
Use --period to control the time window (day, week, month).

AWS credentials: --profile, AWS_PROFILE, or the javabin-team-<team> profile
written by "javabin aws setup" (picked automatically). Costs are AWS USD
amounts shown with their approximate NOK value (team budgets are in NOK).`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().StringVar(&teamFlag, "team", "", "Team name (reads from app.yaml if not set)")
	statusCmd.Flags().StringVar(&serviceFlag, "service", "", "Service name (reads from app.yaml if not set)")
	statusCmd.Flags().BoolVarP(&resourcesFlag, "resources", "r", false, "Show resource-level cost breakdown (requires CUR)")
	statusCmd.Flags().StringVar(&periodFlag, "period", "month", "Time period for resource costs: day, week, month")
}

type appYaml struct {
	Name string `yaml:"name"`
	Team string `yaml:"team"`
}

func readAppYaml() *appYaml {
	data, err := os.ReadFile("app.yaml")
	if err != nil {
		return nil
	}
	var app appYaml
	if err := yaml.Unmarshal(data, &app); err != nil {
		return nil
	}
	return &app
}

func runStatus(cmd *cobra.Command, args []string) error {
	team := teamFlag
	service := serviceFlag

	if team == "" || service == "" {
		if app := readAppYaml(); app != nil {
			if team == "" {
				team = app.Team
			}
			if service == "" {
				service = app.Name
			}
		}
	}

	if team == "" {
		return fmt.Errorf("could not determine team — use --team flag or run from a directory with app.yaml")
	}

	fmt.Printf("Team: %s\n", team)
	if service != "" {
		fmt.Printf("Service: %s\n", service)
	}
	fmt.Println()

	ctx := context.Background()
	// Same profile resolution as `secret`: --profile, AWS_PROFILE, then the
	// javabin-team-<team> profile written by `javabin aws setup`.
	cfg, err := aws.LoadConfigForTeam(ctx, team)
	if err != nil {
		return err
	}

	// Cost this month
	fmt.Println("--- Costs (month-to-date) ---")
	cost, err := aws.GetTeamMonthlyCost(ctx, cfg, team)
	if err != nil {
		fmt.Printf("  Could not fetch costs: %v\n", err)
	} else {
		fmt.Printf("  Team spend: %s\n", formatCost(cost))
	}

	// Resource-level breakdown from CUR
	if resourcesFlag {
		fmt.Printf("\n--- Top Resources (%s) ---\n", periodFlag)
		resources, err := aws.GetTeamResourceCosts(ctx, cfg, team, periodFlag)
		if err != nil {
			fmt.Printf("  Could not fetch resources: %v\n", err)
		} else if len(resources) == 0 {
			fmt.Println("  No CUR data available yet")
		} else {
			for _, r := range resources {
				fmt.Printf("  %-40s %-20s %s\n", r.FriendlyName(), r.Service, formatCost(r.Cost))
			}
		}
	}

	// ECS services
	fmt.Println("\n--- ECS Services ---")
	services, err := aws.ListServices(ctx, cfg, "javabin-platform")
	if err != nil {
		fmt.Printf("  Could not list services: %v\n", err)
	} else {
		prefix := team + "-"
		found := false
		for _, svc := range services {
			if !strings.HasPrefix(svc.Name, prefix) {
				continue
			}
			if service != "" && svc.Name != prefix+service {
				continue
			}
			fmt.Printf("  %s  running=%d desired=%d\n", svc.Name, svc.RunningCount, svc.DesiredCount)
			found = true
		}
		if !found {
			if service != "" {
				fmt.Printf("  No services matching %s%s\n", prefix, service)
			} else {
				fmt.Printf("  No services matching %s*\n", prefix)
			}
		}
	}

	return nil
}

// usdToNOK is the approximate rate the platform uses to turn team budgets
// (budget_nok) into USD AWS Budgets and to report costs
// (platform terraform/lambda-src/shared/constants.py USD_TO_NOK).
const usdToNOK = 10.5

// formatCost shows an AWS (USD) amount next to its NOK equivalent, the
// currency team budgets are set in, like the platform's cost reports.
func formatCost(usd float64) string {
	return fmt.Sprintf("$%.2f (~%.0f NOK)", usd, usd*usdToNOK)
}
