package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/javaBin/javabin-cli/internal/aws"
	"github.com/javaBin/javabin-cli/internal/config"
	"github.com/spf13/cobra"
)

var projectFlag string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show project status (costs, services, deployments)",
	RunE:  runStatus,
}

func init() {
	statusCmd.Flags().StringVar(&projectFlag, "project", "", "Project name (inferred from git remote if not set)")
}

func runStatus(cmd *cobra.Command, args []string) error {
	project := projectFlag
	if project == "" {
		project = inferProject()
	}
	if project == "" {
		return fmt.Errorf("could not infer project name — use --project flag or run from a javaBin repo")
	}

	fmt.Printf("Project: %s\n\n", project)
	ctx := context.Background()

	cfg, err := aws.LoadConfig(ctx)
	if err != nil {
		return fmt.Errorf("AWS credentials not configured: %w", err)
	}

	// Cost this month
	fmt.Println("--- Costs (month-to-date) ---")
	cost, err := aws.GetMonthlyCost(ctx, cfg, project)
	if err != nil {
		fmt.Printf("  Could not fetch costs: %v\n", err)
	} else {
		fmt.Printf("  Spend: $%.2f\n", cost)
	}

	// ECS services
	fmt.Println("\n--- ECS Services ---")
	services, err := aws.ListServices(ctx, cfg, "javabin-platform")
	if err != nil {
		fmt.Printf("  Could not list services: %v\n", err)
	} else if len(services) == 0 {
		fmt.Println("  No running services")
	} else {
		for _, svc := range services {
			if strings.Contains(svc.Name, project) {
				fmt.Printf("  %s  running=%d desired=%d\n", svc.Name, svc.RunningCount, svc.DesiredCount)
			}
		}
	}

	// TODO: Last 5 deployments (requires ECS describe-services with deployments)
	// TODO: Untagged resources (requires Config or resource group tagging API)

	_ = config.EnsureConfigDir()
	return nil
}

func inferProject() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	// Handle both HTTPS and SSH URLs
	// https://github.com/javaBin/moresleep.git -> moresleep
	// git@github.com:javaBin/moresleep.git -> moresleep
	for _, prefix := range []string{
		"https://github.com/javaBin/",
		"git@github.com:javaBin/",
	} {
		if strings.HasPrefix(url, prefix) {
			name := strings.TrimPrefix(url, prefix)
			name = strings.TrimSuffix(name, ".git")
			return name
		}
	}
	return ""
}
