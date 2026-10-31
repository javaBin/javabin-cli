package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	gh "github.com/javaBin/javabin-cli/internal/github"
	"github.com/spf13/cobra"
)

const templateRepo = "app-template"

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a new app repo from the Javabin app template",
	Long:  "Interactive wizard that creates a new repo under javaBin/ from the app-template and customizes it for your runtime.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
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

	// Service name
	name := prompt("Service name (lowercase, e.g. moresleep)", "")
	if name == "" {
		return fmt.Errorf("service name is required")
	}
	name = strings.ToLower(name)
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,19}$`).MatchString(name) {
		return fmt.Errorf("service name must be lowercase alphanumeric with hyphens, start with a letter, max 20 chars")
	}

	// Check repo doesn't already exist
	if gh.RepoExists(token, name) {
		return fmt.Errorf("repository javaBin/%s already exists", name)
	}

	// Team
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

	// Runtime
	runtimeNames := []string{"java", "kotlin", "typescript", "python", "go"}
	fmt.Printf("\nRuntime options: %s\n", strings.Join(runtimeNames, ", "))
	runtime := strings.ToLower(prompt("Runtime", "java"))
	rc, ok := runtimes[runtime]
	if !ok {
		return fmt.Errorf("unsupported runtime: %s (choose from: %s)", runtime, strings.Join(runtimeNames, ", "))
	}

	// Port
	port := prompt("Port", rc.defaultPort)

	// Routing — required for a web service to get a URL and ALB rule
	// (routing.priority is left out: the platform derives one.)
	host := prompt("Hostname", name+".javazone.no")

	// Visibility
	visibility := prompt("Public repo? (y/n)", "n")
	private := strings.ToLower(visibility) != "y"

	// Confirm
	fmt.Println("\n--- New App Summary ---")
	fmt.Printf("  Name:    %s\n", name)
	fmt.Printf("  Team:    %s\n", team)
	fmt.Printf("  Runtime: %s\n", runtime)
	fmt.Printf("  Port:    %s\n", port)
	fmt.Printf("  Host:    %s\n", host)
	if private {
		fmt.Println("  Repo:    private")
	} else {
		fmt.Println("  Repo:    public")
	}
	fmt.Println()

	confirm := prompt("Create repo and scaffold? (y/n)", "y")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled.")
		return nil
	}

	// Create repo from template
	fmt.Printf("\nCreating javaBin/%s from template... ", name)
	cloneURL, err := gh.CreateRepoFromTemplate(token, templateRepo, name, fmt.Sprintf("%s service (%s)", name, team), private)
	if err != nil {
		fmt.Println("failed")
		return fmt.Errorf("create repo from template: %w", err)
	}
	fmt.Println("done")

	// Clone locally
	fmt.Printf("Cloning into ./%s... ", name)
	cloneCmd := exec.Command("git", "clone", cloneURL)
	cloneCmd.Stdout = nil
	cloneCmd.Stderr = nil
	if err := cloneCmd.Run(); err != nil {
		fmt.Println("failed")
		return fmt.Errorf("git clone: %w", err)
	}
	fmt.Println("done")

	repoDir := filepath.Join(".", name)

	// Write app.yaml, the runtime scaffold and the deploy workflow
	files := rc.render(name, port)
	files["app.yaml"] = renderAppYaml(name, team, port, host)
	files[".github/workflows/deploy.yml"] = deployYaml
	written, err := writeFiles(repoDir, files)
	if err != nil {
		return fmt.Errorf("write scaffold: %w", err)
	}
	for _, f := range written {
		fmt.Printf("  wrote %s\n", f)
	}

	fmt.Print("Committing... ")
	if err := runGit(repoDir, append([]string{"add", "--"}, written...)...); err != nil {
		fmt.Println("failed")
		return err
	}
	if err := runGit(repoDir, "commit", "-m", fmt.Sprintf("Configure %s for Javabin platform", name)); err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Println("done")

	// The platform only deploys repos that belong to the app.yaml team, so
	// the repo must be in the GitHub team before the first push to main
	// triggers the pipeline.
	fmt.Printf("Adding javaBin/%s to GitHub team %s... ", name, team)
	if err := gh.AddRepoToTeam(token, team, name); err != nil {
		fmt.Println("failed")
		return teamAccessError(err, team, name)
	}
	fmt.Println("done")

	fmt.Print("Pushing to main... ")
	if err := runGit(repoDir, "push"); err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Println("done")

	fmt.Printf("\nRepo ready: https://github.com/javaBin/%s\n", name)
	fmt.Println("The push to main started the platform pipeline (Actions tab).")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. For AWS access (logs, status, cost), set up your team's SSO profile:")
	fmt.Println("     javabin aws setup")
	fmt.Println("  2. Secrets: list them under resources.secrets in app.yaml, deploy, then set each value:")
	fmt.Println("     javabin secret set <name>   (from the repo dir)")

	return nil
}

func runGit(dir string, args ...string) error {
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", args[0], err, string(out))
	}
	return nil
}

// teamAccessError explains why the repo could not be added to the team. The
// scaffold is committed locally but not pushed, so the first pipeline run is
// not rejected for a repo outside the team.
func teamAccessError(err error, team, name string) error {
	addCmd := fmt.Sprintf("gh api -X PUT orgs/javaBin/teams/%s/repos/javaBin/%s -f permission=push", team, name)
	var why string
	if errors.Is(err, gh.ErrNoTeamAccess) {
		why = fmt.Sprintf("you are not allowed to add repos to team %s (only its maintainers, role: maintainer in\n"+
			"javaBin/registry teams/%s.yaml, and org owners can), or the team does not exist yet", team, team)
	} else {
		why = err.Error()
	}
	return fmt.Errorf("could not add javaBin/%s to GitHub team %s: %s\n\n"+
		"The scaffold is committed in ./%s but NOT pushed: the platform rejects deploys from repos outside the app.yaml team.\n"+
		"Ask a team maintainer or org owner to run:\n  %s\nthen push:\n  cd %s && git push", name, team, why, name, addCmd, name)
}

// deployYaml is the app's CI entrypoint. The permissions are required:
// id-token for AWS OIDC, contents: write so the platform's expand step can
// commit generated Terraform, pull-requests for plan comments.
const deployYaml = `name: Deploy
on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:

permissions:
  id-token: write
  contents: write
  pull-requests: write

jobs:
  javabin:
    uses: javaBin/platform/.github/workflows/javabin.yml@main
`

// renderAppYaml writes the minimal app.yaml for a routed web service.
// routing.priority is omitted; the platform derives a unique one.
func renderAppYaml(name, team, port, host string) string {
	return fmt.Sprintf(`name: %s
team: %s

compute:
  port: %s
  health_check: /health

routing:
  host: %s
`, name, team, port, host)
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
