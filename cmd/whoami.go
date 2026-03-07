package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/javaBin/javabin-cli/internal/aws"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current identity (AWS + GitHub)",
	RunE:  runWhoami,
}

func runWhoami(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// AWS identity
	fmt.Println("--- AWS Identity ---")
	cfg, err := aws.LoadConfig(ctx)
	if err != nil {
		fmt.Printf("  Not authenticated: %v\n", err)
	} else {
		identity, err := aws.GetCallerIdentity(ctx, cfg)
		if err != nil {
			fmt.Printf("  Could not get identity: %v\n", err)
		} else {
			fmt.Printf("  Account: %s\n", identity.Account)
			fmt.Printf("  ARN:     %s\n", identity.ARN)
			fmt.Printf("  UserID:  %s\n", identity.UserID)
		}
	}

	// GitHub identity
	fmt.Println("\n--- GitHub Identity ---")
	ghUser := getGitHubUser()
	if ghUser != "" {
		fmt.Printf("  User: %s\n", ghUser)
	} else {
		fmt.Println("  Not authenticated (run 'gh auth login' or set GITHUB_TOKEN)")
	}

	// TODO: Cognito identity — will be added when Cognito user pools are
	// implemented (Phase 1 Identity). Device flow auth against internal pool,
	// token cached at ~/.javabin/token.

	return nil
}

func getGitHubUser() string {
	out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
