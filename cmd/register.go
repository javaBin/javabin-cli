package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/javaBin/javabin-cli/internal/config"
	gh "github.com/javaBin/javabin-cli/internal/github"
	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register-team",
	Short: "Register a new team with the Javabin platform",
	Long:  "Interactive wizard that creates a team registration PR against javaBin/registry.",
	RunE:  runRegister,
}

type member struct {
	Google string
	GitHub string
}

func runRegister(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)
	prompt := func(label, defaultVal string) string {
		if defaultVal != "" {
			fmt.Printf("%s [%s]: ", label, defaultVal)
		} else {
			fmt.Printf("%s: ", label)
		}
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			return defaultVal
		}
		return input
	}

	token, err := gh.GetToken()
	if err != nil {
		return fmt.Errorf("GitHub auth required: %w", err)
	}

	// Team name
	teamName := prompt("Team name (lowercase, e.g. video)", "")
	if teamName == "" {
		return fmt.Errorf("team name is required")
	}
	teamName = strings.ToLower(teamName)

	// Description
	description := prompt("Description (what the team does)", "")
	if description == "" {
		return fmt.Errorf("description is required")
	}

	// Members
	fmt.Println("\nAdd team members (at least one). Leave blank to stop.")
	var members []member
	for i := 1; ; i++ {
		fmt.Printf("\n--- Member %d ---\n", i)
		google := prompt("Google handle (firstname.lastname)", "")
		if google == "" {
			if len(members) == 0 {
				fmt.Println("At least one member is required.")
				continue
			}
			break
		}
		github := prompt("GitHub username", "")
		if github == "" {
			fmt.Println("GitHub username is required for each member.")
			continue
		}
		members = append(members, member{Google: google, GitHub: github})
	}

	// Budget
	budget := prompt("\nMonthly budget (NOK)", "500")

	// Confirm
	fmt.Println("\n--- Team Registration Summary ---")
	fmt.Printf("  Name:         %s\n", teamName)
	fmt.Printf("  Description:  %s\n", description)
	fmt.Printf("  Google Group: team-%s@java.no\n", teamName)
	fmt.Printf("  Budget:       %s NOK/mo\n", budget)
	fmt.Println("  Members:")
	for _, m := range members {
		fmt.Printf("    - %s (github: %s)\n", m.Google, m.GitHub)
	}
	fmt.Println()

	confirm := prompt("Create registration PR? (y/n)", "y")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled.")
		return nil
	}

	// Build team YAML content
	var yamlLines []string
	yamlLines = append(yamlLines, fmt.Sprintf("name: %s", teamName))
	yamlLines = append(yamlLines, fmt.Sprintf("description: %s", description))
	yamlLines = append(yamlLines, "members:")
	for _, m := range members {
		yamlLines = append(yamlLines, fmt.Sprintf("  - google: %s", m.Google))
		yamlLines = append(yamlLines, fmt.Sprintf("    github: %s", m.GitHub))
	}
	if budget != "500" && budget != "" {
		yamlLines = append(yamlLines, fmt.Sprintf("budget_nok: %s", budget))
	}
	yamlContent := strings.Join(yamlLines, "\n") + "\n"

	// Create PR via GitHub API
	filePath := fmt.Sprintf("teams/%s.yaml", teamName)
	branchName := fmt.Sprintf("register-team-%s", teamName)
	prTitle := fmt.Sprintf("Register team %s", teamName)
	prBody := fmt.Sprintf("Register team `%s`.\n\n**Description:** %s\n\n**Members:**\n", teamName, description)
	for _, m := range members {
		prBody += fmt.Sprintf("- %s (@%s)\n", m.Google, m.GitHub)
	}
	prBody += fmt.Sprintf("\nGoogle Group: `team-%s@java.no`\n\nCreated by `javabin register-team`.", teamName)

	prURL, err := gh.CreateRegistrationPR(token, branchName, filePath, yamlContent, prTitle, prBody)
	if err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}

	fmt.Printf("\nRegistration PR created: %s\n", prURL)
	fmt.Println("A platform owner will review and merge it.")
	fmt.Println("\nAfter your team is created, add repos to your GitHub team:")
	fmt.Printf("  gh api orgs/javaBin/teams/%s/repos -f owner=javaBin -f repo=REPO -f permission=push\n", teamName)

	_ = config.EnsureConfigDir()
	return nil
}
