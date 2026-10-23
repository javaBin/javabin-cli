package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	jbaws "github.com/javaBin/javabin-cli/internal/aws"
)

const (
	project     = "javabin"
	clusterName = "javabin-platform"
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
	if app.Name == "" || app.Team == "" {
		return nil, fmt.Errorf("%s must set name and team", path)
	}
	return &app, nil
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
	secretRestart bool
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "List and set the app's secrets (app.yaml resources.secrets)",
	Long: `Secrets declared in app.yaml under resources.secrets are created by the
platform with a PLACEHOLDER value. Set the real values here; they are stored as
SSM SecureStrings under /javabin/apps/<team>/<app>/ and injected into the
container at task start. Needs AWS SSO credentials (javabin-developer).`,
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
		cfg, err := jbaws.LoadConfig(ctx)
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

// secretStatus never prints the value, only whether it is set.
func secretStatus(ctx context.Context, client *ssm.Client, path string) string {
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
		cfg, err := jbaws.LoadConfig(ctx)
		if err != nil {
			return err
		}
		path := secretPath(app, name)
		if _, err := ssm.NewFromConfig(cfg).PutParameter(ctx, &ssm.PutParameterInput{
			Name:      aws.String(path),
			Value:     aws.String(value),
			Type:      ssmtypes.ParameterTypeSecureString,
			Overwrite: aws.Bool(true),
		}); err != nil {
			return fmt.Errorf("set %s: %w", path, err)
		}
		fmt.Printf("Set %s\n", path)

		service := app.Team + "-" + app.Name
		if !secretRestart {
			fmt.Printf("Running tasks keep the old value until they restart. Re-run with --restart, or:\n  aws ecs update-service --cluster %s --service %s --force-new-deployment\n", clusterName, service)
			return nil
		}
		if _, err := ecs.NewFromConfig(cfg).UpdateService(ctx, &ecs.UpdateServiceInput{
			Cluster:            aws.String(clusterName),
			Service:            aws.String(service),
			ForceNewDeployment: true,
		}); err != nil {
			return fmt.Errorf("restart %s: %w", service, err)
		}
		fmt.Printf("Restarting %s — new tasks get the new value.\n", service)
		return nil
	},
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
		data, err := term.ReadPassword(int(f.Fd()))
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

func init() {
	secretCmd.PersistentFlags().StringVar(&secretAppYaml, "app-yaml", "app.yaml", "path to app.yaml")
	secretSetCmd.Flags().BoolVar(&secretStdin, "stdin", false, "read the value from stdin (e.g. from a password manager)")
	secretSetCmd.Flags().BoolVar(&secretRestart, "restart", false, "restart the ECS service so tasks pick up the new value")
	secretCmd.AddCommand(secretListCmd, secretSetCmd)
	rootCmd.AddCommand(secretCmd)
}
