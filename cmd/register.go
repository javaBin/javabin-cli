package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/javaBin/javabin-cli/internal/config"
	gh "github.com/javaBin/javabin-cli/internal/github"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var registerCmd = &cobra.Command{
	Use:   "register-team",
	Short: "Register a new team with the Javabin platform",
	Long:  "Interactive wizard that creates a team registration PR against javaBin/registry.",
	RunE:  runRegister,
}

type member struct {
	Google string `yaml:"google"`
	GitHub string `yaml:"github"`
	// Role is "maintainer" for the first member (the person registering),
	// who can then add repos to the GitHub team; others default to member.
	Role string `yaml:"role,omitempty"`
}

type teamYaml struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Members     []member `yaml:"members"`
	BudgetNOK   *int     `yaml:"budget_nok,omitempty"`
}

// renderTeamYaml serialises teams/<name>.yaml for javaBin/registry. The first
// member is the team maintainer. budget_nok is omitted at the default (500).
func renderTeamYaml(name, description string, members []member, budget string) (string, error) {
	t := teamYaml{Name: name, Description: description}
	for i, m := range members {
		if i == 0 {
			m.Role = "maintainer"
		}
		t.Members = append(t.Members, m)
	}
	if budget != "" && budget != "500" {
		n, err := strconv.Atoi(budget)
		if err != nil || n <= 0 {
			return "", fmt.Errorf("budget must be a positive whole number of NOK, got %q", budget)
		}
		t.BudgetNOK = &n
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
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
	if _, err := renderTeamYaml(teamName, description, members, budget); err != nil {
		return err
	}

	// Confirm
	fmt.Println("\n--- Team Registration Summary ---")
	fmt.Printf("  Name:         %s\n", teamName)
	fmt.Printf("  Description:  %s\n", description)
	fmt.Printf("  Google Group: team-%s@java.no\n", teamName)
	fmt.Printf("  Budget:       %s NOK/mo\n", budget)
	fmt.Println("  Members:")
	for i, m := range members {
		role := ""
		if i == 0 {
			role = ", maintainer"
		}
		fmt.Printf("    - %s (github: %s%s)\n", m.Google, m.GitHub, role)
	}
	fmt.Println()

	confirm := prompt("Create registration PR? (y/n)", "y")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled.")
		return nil
	}

	yamlContent, err := renderTeamYaml(teamName, description, members, budget)
	if err != nil {
		return fmt.Errorf("render team yaml: %w", err)
	}

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
	fmt.Printf("  gh api -X PUT orgs/javaBin/teams/%s/repos/javaBin/REPO -f permission=push\n", teamName)
	fmt.Printf("(%s is the team maintainer and can do this; `javabin init` does it for new repos.)\n", members[0].GitHub)

	_ = config.EnsureConfigDir()
	return nil
}
