package cmd

import (
	"bufio"
	"encoding/json"
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

type runtimeConfig struct {
	defaultPort string
	dockerfile  string
}

var runtimes = map[string]runtimeConfig{
	"java": {
		defaultPort: "8080",
		dockerfile: `FROM eclipse-temurin:21-jdk-alpine AS build
WORKDIR /app
COPY pom.xml .
COPY src ./src
RUN apk add --no-cache maven && mvn package -DskipTests

FROM eclipse-temurin:21-jre-alpine
COPY --from=build /app/target/*.jar /app/app.jar
EXPOSE 8080
CMD ["java", "-jar", "/app/app.jar"]
`,
	},
	"kotlin": {
		defaultPort: "8080",
		dockerfile: `FROM eclipse-temurin:21-jdk-alpine AS build
WORKDIR /app
COPY . .
# installDist names the distribution after the Gradle project; normalise it
# to /out with a stable bin/start entrypoint.
RUN ./gradlew --no-daemon installDist -x test \
    && mkdir /out && cp -r build/install/*/. /out/ \
    && ln -s "$(ls /out/bin | grep -v '\.bat$' | head -1)" /out/bin/start

FROM eclipse-temurin:21-jre-alpine
COPY --from=build /out /opt/app
EXPOSE 8080
CMD ["/opt/app/bin/start"]
`,
	},
	"typescript": {
		defaultPort: "3000",
		dockerfile: `FROM node:22-alpine AS build
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM node:22-alpine
WORKDIR /app
COPY --from=build /app/dist ./dist
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/package.json .
EXPOSE 3000
CMD ["node", "dist/index.js"]
`,
	},
	"python": {
		defaultPort: "8000",
		dockerfile: `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "-m", "uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
`,
	},
	"go": {
		defaultPort: "8080",
		dockerfile: `FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server .

FROM alpine:3.19
COPY --from=build /app/server /server
EXPOSE 8080
CMD ["/server"]
`,
	},
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
	host := prompt("Hostname", name+".javazone.no")
	priority := prompt("ALB rule priority (unique across apps, 1-50000)", "")
	if !regexp.MustCompile(`^[1-9][0-9]{0,4}$`).MatchString(priority) {
		return fmt.Errorf("priority must be a number between 1 and 50000")
	}

	// Visibility
	visibility := prompt("Public repo? (y/n)", "n")
	private := strings.ToLower(visibility) != "y"

	// Confirm
	fmt.Println("\n--- New App Summary ---")
	fmt.Printf("  Name:    %s\n", name)
	fmt.Printf("  Team:    %s\n", team)
	fmt.Printf("  Runtime: %s\n", runtime)
	fmt.Printf("  Port:    %s\n", port)
	fmt.Printf("  Host:    %s (priority %s)\n", host, priority)
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

	// Write app.yaml
	if err := os.WriteFile(filepath.Join(repoDir, "app.yaml"), []byte(renderAppYaml(name, team, port, host, priority)), 0644); err != nil {
		return fmt.Errorf("write app.yaml: %w", err)
	}
	fmt.Println("  wrote app.yaml")

	// Write Dockerfile
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte(rc.dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}
	fmt.Println("  wrote Dockerfile")

	// Write deploy workflow
	workflowDir := filepath.Join(repoDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return fmt.Errorf("create workflow dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "deploy.yml"), []byte(deployYaml), 0644); err != nil {
		return fmt.Errorf("write deploy.yml: %w", err)
	}
	fmt.Println("  wrote .github/workflows/deploy.yml")

	// Commit and push
	fmt.Print("Committing and pushing... ")
	gitCmds := [][]string{
		{"add", "app.yaml", "Dockerfile", ".github/workflows/deploy.yml"},
		{"commit", "-m", fmt.Sprintf("Configure %s for Javabin platform", name)},
		{"push"},
	}
	for _, gitArgs := range gitCmds {
		c := exec.Command("git", gitArgs...)
		c.Dir = repoDir
		if out, err := c.CombinedOutput(); err != nil {
			fmt.Println("failed")
			return fmt.Errorf("git %s: %w\n%s", gitArgs[0], err, string(out))
		}
	}
	fmt.Println("done")

	fmt.Printf("\nRepo ready: https://github.com/javaBin/%s\n", name)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Add this repo to your GitHub team (the platform deploys as the app.yaml team):")
	fmt.Printf("     gh api -X PUT orgs/javaBin/teams/%s/repos/javaBin/%s -f permission=push\n", team, name)
	fmt.Println("  2. Push to main — the platform CI takes over")
	fmt.Println("  3. For AWS access (logs, status, cost), set up your team's SSO profile:")
	fmt.Println("     javabin aws setup")
	fmt.Println("  4. Secrets: list them under resources.secrets in app.yaml, deploy, then set each value:")
	fmt.Println("     javabin secret set <name>   (from the repo dir)")

	return nil
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
func renderAppYaml(name, team, port, host, priority string) string {
	return fmt.Sprintf(`name: %s
team: %s

compute:
  port: %s
  health_check: /health

routing:
  host: %s
  priority: %s
`, name, team, port, host, priority)
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
