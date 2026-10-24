package aws

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// AccountID is the javaBin platform account.
const AccountID = "553637109631"

// Profile is set from the global --profile flag.
var Profile string

// LoadConfigForTeam loads AWS config for working on a team's resources.
// Profile resolution: --profile, then AWS_PROFILE, then the ~/.aws/config
// SSO profile for this account whose role is javabin-team-<team>. Without
// any of those the SDK would fall through to EC2 instance metadata and fail
// with a confusing timeout, so we say what to do instead.
func LoadConfigForTeam(ctx context.Context, team string) (aws.Config, error) {
	profile := Profile
	if profile == "" && os.Getenv("AWS_PROFILE") == "" && os.Getenv("AWS_ACCESS_KEY_ID") == "" && team != "" {
		profile = FindTeamProfile(awsConfigPath(), team)
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(defaultRegion)}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return cfg, err
	}
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		return cfg, credentialsHelp(err, profile, team)
	}
	return cfg, nil
}

func credentialsHelp(err error, profile, team string) error {
	role := "javabin-team-" + team
	if profile != "" {
		return fmt.Errorf("no valid AWS credentials for profile %q — run: aws sso login --profile %s\n(%w)", profile, profile, err)
	}
	return errors.New("no AWS credentials found. Sign in with your team's SSO role (" + role + "):\n" +
		"  aws configure sso          # once: account " + AccountID + ", role " + role + "\n" +
		"  aws sso login --profile <name>\n" +
		"then pass --profile <name> or set AWS_PROFILE=<name>")
}

func awsConfigPath() string {
	if p := os.Getenv("AWS_CONFIG_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", "config")
}

// FindTeamProfile returns the profile in an AWS config file whose SSO role
// is javabin-team-<team> in the platform account, or "" (also when several
// match, to avoid guessing).
func FindTeamProfile(path, team string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	want := "javabin-team-" + team
	var matches []string
	var name string
	vals := map[string]string{}
	flush := func() {
		if name != "" && vals["sso_role_name"] == want && (vals["sso_account_id"] == "" || vals["sso_account_id"] == AccountID) {
			matches = append(matches, name)
		}
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section := strings.TrimSpace(line[1 : len(line)-1])
			name, vals = "", map[string]string{}
			if section == "default" {
				name = "default"
			} else if p, ok := strings.CutPrefix(section, "profile "); ok {
				name = strings.TrimSpace(p)
			}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	flush()
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
