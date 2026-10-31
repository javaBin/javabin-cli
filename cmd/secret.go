package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	jbaws "github.com/javaBin/javabin-cli/internal/aws"
)

const (
	project     = "javabin"
	placeholder = "PLACEHOLDER"
)

// appSecrets is the part of app.yaml the secret commands need.
type appSecrets struct {
	Name      string `yaml:"name"`
	Team      string `yaml:"team"`
	Resources struct {
		Secrets []struct {
			Name string `yaml:"name"`
			Env  string `yaml:"env"`
		} `yaml:"secrets"`
	} `yaml:"resources"`
}

func loadAppYaml(path string) (*appSecrets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s (run from the app repo root): %w", path, err)
	}
	var app appSecrets
	if err := yaml.Unmarshal(data, &app); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if app.Team == "" {
		return nil, fmt.Errorf("%s must set team", path)
	}
	if app.Name == "" {
		// The platform defaults name to the repo name, but only accepts it if
		// it is a valid app name.
		repo := repoName(filepath.Dir(path))
		if appNameRE.MatchString(repo) {
			app.Name = repo
		} else {
			return nil, missingNameError(path, repo)
		}
	}
	if !appNameRE.MatchString(app.Name) {
		return nil, fmt.Errorf("%s: name %q must start with a letter and use lowercase letters, digits and hyphens, max 20 chars%s",
			path, app.Name, suggestName(app.Name))
	}
	return &app, nil
}

// appNameRE is the platform's app.yaml name rule (generate-modules.py NAME_RE).
var appNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,19}$`)

func missingNameError(path, repo string) error {
	if repo == "" {
		return fmt.Errorf("%s has no name; set name: (lowercase letters, digits, hyphens, max 20 chars)", path)
	}
	return fmt.Errorf("%s has no name, and the repo name %q is not a valid app name (the platform pipeline rejects it too);\n"+
		"add a name to app.yaml (lowercase letters, digits, hyphens, start with a letter, max 20 chars)%s", path, repo, suggestName(repo))
}

func suggestName(s string) string {
	if n := sanitizeAppName(s); n != "" {
		return fmt.Sprintf(", e.g.\n  name: %s", n)
	}
	return ""
}

// sanitizeAppName turns a repo name like "2027.javazone.no" into a valid app
// name ("javazone-no-2027"): lowercase, non-alphanumerics become hyphens,
// leading numeric parts move to the end, max 20 chars. Returns "" if nothing
// usable is left.
func sanitizeAppName(s string) string {
	parts := regexp.MustCompile(`[^a-z0-9]+`).Split(strings.ToLower(s), -1)
	var words, numbers []string
	for _, p := range parts {
		switch {
		case p == "":
		case len(words) == 0 && (p[0] < 'a' || p[0] > 'z'):
			numbers = append(numbers, p)
		default:
			words = append(words, p)
		}
	}
	if len(words) == 0 {
		return ""
	}
	name := strings.Join(append(words, numbers...), "-")
	if len(name) > 20 {
		name = strings.TrimRight(name[:20], "-")
	}
	return name
}

// repoName is the GitHub repo name of the checkout in dir (from the origin
// remote), falling back to the directory name.
func repoName(dir string) string {
	if out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output(); err == nil {
		url := strings.TrimSuffix(strings.TrimSpace(string(out)), ".git")
		if i := strings.LastIndexAny(url, "/:"); i >= 0 && i < len(url)-1 {
			return url[i+1:]
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	return filepath.Base(abs)
}

// secretPath is where app.yaml `resources.secrets` entries live in SSM
// (service-secret module).
func secretPath(app *appSecrets, name string) string {
	return fmt.Sprintf("/%s/apps/%s/%s/%s", project, app.Team, app.Name, name)
}

func (a *appSecrets) declares(name string) bool {
	for _, s := range a.Resources.Secrets {
		if s.Name == name {
			return true
		}
	}
	return false
}

var (
	secretAppYaml string
	secretStdin   bool
)

// ssmAPI is the subset of the SSM client the secret commands use (fakeable in tests).
type ssmAPI interface {
	GetParameter(ctx context.Context, in *ssm.GetParameterInput, opts ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	PutParameter(ctx context.Context, in *ssm.PutParameterInput, opts ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
}

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "List and set the app's secrets (app.yaml resources.secrets)",
	Long: `Secrets declared in app.yaml under resources.secrets are created by the
platform with a PLACEHOLDER value. Set the real values here; they are stored as
SSM SecureStrings under /javabin/apps/<team>/<app>/ and injected into the
container at task start. Needs your team's AWS SSO role, javabin-team-<team>
(run "javabin aws setup" once; the profile is picked from app.yaml's team).
javabin-developer cannot reach team secrets.`,
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show which declared secrets still have the PLACEHOLDER value",
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := loadAppYaml(secretAppYaml)
		if err != nil {
			return err
		}
		ctx := context.Background()
		cfg, err := jbaws.LoadConfigForTeam(ctx, app.Team)
		if err != nil {
			return err
		}
		client := ssm.NewFromConfig(cfg)
		if len(app.Resources.Secrets) == 0 {
			fmt.Println("app.yaml declares no resources.secrets")
			return nil
		}
		for _, s := range app.Resources.Secrets {
			fmt.Printf("%-24s %-28s %s\n", s.Name, s.Env, secretStatus(ctx, client, secretPath(app, s.Name)))
		}
		return nil
	},
}

// secretStatus never prints the value, only whether it is set. Decrypting is
// deliberate: it is the only way to compare against PLACEHOLDER. The value is
// never printed or stored.
func secretStatus(ctx context.Context, client ssmAPI, path string) string {
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String(path), WithDecryption: aws.Bool(true)})
	var notFound *ssmtypes.ParameterNotFound
	switch {
	case errors.As(err, &notFound):
		return "not created yet (deploy first)"
	case err != nil:
		return "error: " + err.Error()
	case aws.ToString(out.Parameter.Value) == placeholder:
		return "PLACEHOLDER — not set"
	default:
		return fmt.Sprintf("set (version %d)", out.Parameter.Version)
	}
}

var secretSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Set a secret's value (prompted, or from stdin with --stdin)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		app, err := loadAppYaml(secretAppYaml)
		if err != nil {
			return err
		}
		if !app.declares(name) {
			return fmt.Errorf("%q is not in app.yaml resources.secrets — add it and deploy first", name)
		}

		value, err := readSecretValue(os.Stdin, secretStdin, name)
		if err != nil {
			return err
		}

		ctx := context.Background()
		cfg, err := jbaws.LoadConfigForTeam(ctx, app.Team)
		if err != nil {
			return err
		}
		path := secretPath(app, name)
		if err := setSecret(ctx, ssm.NewFromConfig(cfg), path, value); err != nil {
			return err
		}
		fmt.Printf("Set %s\n", path)
		fmt.Printf("The platform will restart the service %s-%s within a minute so new tasks get the new value.\n", app.Team, app.Name)
		return nil
	},
}

// setSecret overwrites an existing secret parameter. It never creates one: the
// platform creates declared secrets on deploy (and IAM denies writes to
// parameters not tagged with the team).
func setSecret(ctx context.Context, client ssmAPI, path, value string) error {
	_, err := client.GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String(path)})
	var notFound *ssmtypes.ParameterNotFound
	if errors.As(err, &notFound) {
		return fmt.Errorf("%s does not exist yet — deploy the app first (the platform creates declared secrets)", path)
	}
	if err != nil {
		return fmt.Errorf("check %s: %w", path, err)
	}
	if _, err := client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(path),
		Value:     aws.String(value),
		Type:      ssmtypes.ParameterTypeSecureString,
		Overwrite: aws.Bool(true),
	}); err != nil {
		return fmt.Errorf("set %s: %w", path, err)
	}
	return nil
}

// readSecretValue reads one value without echoing it on a terminal.
func readSecretValue(in io.Reader, fromStdin bool, name string) (string, error) {
	var value string
	if fromStdin {
		data, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		value = strings.TrimRight(string(data), "\r\n")
	} else if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Printf("Value for %s (input hidden): ", name)
		data, err := readPasswordRestoringOnInterrupt(int(f.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		value = string(data)
	} else {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		value = strings.TrimRight(line, "\r\n")
	}
	if value == "" || value == placeholder {
		return "", fmt.Errorf("refusing to set an empty or %s value", placeholder)
	}
	return value, nil
}

// readPasswordRestoringOnInterrupt reads hidden input and, if the user hits
// Ctrl-C mid-prompt, restores the terminal (echo on) before exiting non-zero.
func readPasswordRestoringOnInterrupt(fd int) ([]byte, error) {
	state, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	done := make(chan struct{})
	defer func() {
		signal.Stop(sigs)
		close(done)
	}()
	go func() {
		select {
		case <-sigs:
			_ = term.Restore(fd, state)
			fmt.Println()
			os.Exit(130)
		case <-done:
		}
	}()
	return term.ReadPassword(fd)
}

func init() {
	secretCmd.PersistentFlags().StringVar(&secretAppYaml, "app-yaml", "app.yaml", "path to app.yaml")
	secretSetCmd.Flags().BoolVar(&secretStdin, "stdin", false, "read the value from stdin (e.g. from a password manager)")
	secretCmd.AddCommand(secretListCmd, secretSetCmd)
	rootCmd.AddCommand(secretCmd)
}
