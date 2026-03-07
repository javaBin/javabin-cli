package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/javaBin/javabin-cli/internal/config"
	gh "github.com/javaBin/javabin-cli/internal/github"
	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new app with the Javabin platform",
	Long:  "Interactive wizard that creates a registration PR against javaBin/registry.",
	RunE:  runRegister,
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

	// Repo name
	repoName := prompt("Repository name (e.g. moresleep)", "")
	if repoName == "" {
		return fmt.Errorf("repository name is required")
	}

	// Validate repo exists
	fmt.Printf("Checking javaBin/%s exists... ", repoName)
	if !repoExists(token, repoName) {
		fmt.Println("not found")
		return fmt.Errorf("repository javaBin/%s does not exist", repoName)
	}
	fmt.Println("ok")

	// List teams from registry
	fmt.Println("\nAvailable teams:")
	teams, err := listTeams(token)
	if err != nil {
		fmt.Printf("  (could not fetch teams: %v)\n", err)
	} else {
		for _, t := range teams {
			fmt.Printf("  - %s\n", t)
		}
	}
	team := prompt("\nTeam", "")
	if team == "" {
		return fmt.Errorf("team is required")
	}

	// Auth
	fmt.Println("\nAuth options: internal, external, both, none")
	auth := prompt("Auth", "none")

	// Budget
	budget := prompt("Monthly budget (NOK)", "1000")

	// Dev environment
	devEnv := prompt("Need a dev environment? (y/n)", "n")

	// Confirm
	fmt.Println("\n--- Registration Summary ---")
	fmt.Printf("  Repo:   javaBin/%s\n", repoName)
	fmt.Printf("  Team:   %s\n", team)
	fmt.Printf("  Auth:   %s\n", auth)
	fmt.Printf("  Budget: %s NOK\n", budget)
	if strings.ToLower(devEnv) == "y" {
		fmt.Println("  Dev:    yes")
	}
	fmt.Println()

	confirm := prompt("Create registration PR? (y/n)", "y")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled.")
		return nil
	}

	// Build app YAML content
	var yamlLines []string
	yamlLines = append(yamlLines, fmt.Sprintf("name: %s", repoName))
	yamlLines = append(yamlLines, fmt.Sprintf("team: %s", team))
	yamlLines = append(yamlLines, fmt.Sprintf("repo: javaBin/%s", repoName))
	if auth != "none" && auth != "" {
		yamlLines = append(yamlLines, fmt.Sprintf("auth: %s", auth))
	}
	if budget != "1000" && budget != "" {
		yamlLines = append(yamlLines, fmt.Sprintf("budget_alert_nok: %s", budget))
	}
	yamlContent := strings.Join(yamlLines, "\n") + "\n"

	// Create PR via GitHub API
	filePath := fmt.Sprintf("apps/%s.yaml", repoName)
	branchName := fmt.Sprintf("register-%s", repoName)
	prTitle := fmt.Sprintf("Register %s", repoName)
	prBody := fmt.Sprintf("Register `javaBin/%s` with team `%s`.\n\nCreated by `javabin register`.", repoName, team)

	prURL, err := gh.CreateRegistrationPR(token, branchName, filePath, yamlContent, prTitle, prBody)
	if err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}

	fmt.Printf("\nRegistration PR created: %s\n", prURL)
	fmt.Println("A platform owner will review and merge it.")

	_ = config.EnsureConfigDir()
	return nil
}

func repoExists(token, name string) bool {
	req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/javaBin/%s", name), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func listTeams(token string) ([]string, error) {
	url := "https://api.github.com/repos/javaBin/registry/contents/teams"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var items []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	var teams []string
	for _, item := range items {
		if strings.HasSuffix(item.Name, ".yaml") {
			teams = append(teams, strings.TrimSuffix(item.Name, ".yaml"))
		}
	}
	return teams, nil
}
