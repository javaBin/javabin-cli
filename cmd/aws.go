package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	jbaws "github.com/javaBin/javabin-cli/internal/aws"
	"github.com/javaBin/javabin-cli/internal/awsconfig"
)

var (
	awsSetupForce   bool
	awsSetupNoLogin bool
	awsSetupDryRun  bool
	awsDoctorYaml   string
)

// awsDeps is everything `aws setup` touches outside the config file, so
// tests can swap in fakes.
type awsDeps struct {
	configPath string
	cacheDir   string
	login      func() error
	roles      awsconfig.RoleLister
	now        func() time.Time
	out        io.Writer
}

func defaultAwsDeps() awsDeps {
	return awsDeps{
		configPath: awsconfig.DefaultPath(),
		cacheDir:   awsconfig.DefaultCacheDir(),
		login:      ssoLogin,
		roles:      awsconfig.SSORoleLister{Region: awsconfig.SSORegion},
		now:        time.Now,
		out:        os.Stdout,
	}
}

var awsCmd = &cobra.Command{
	Use:   "aws",
	Short: "Set up and check AWS SSO access to the javaBin account",
	// errors here are actionable messages, not usage mistakes
	PersistentPreRun: func(cmd *cobra.Command, args []string) { cmd.SilenceUsage = true },
}

var awsSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Write the javaBin SSO session and one profile per role you have to ~/.aws/config",
	Long: `Adds an [sso-session javabin] block for javaBin's IAM Identity Center to the
AWS config file (AWS_CONFIG_FILE or ~/.aws/config), signs in with
'aws sso login --sso-session javabin', lists the roles you are assigned in
account ` + awsconfig.AccountID + ` and writes one profile per role.

Profiles are named after the role: javabin-team-<team>, javabin-developer,
javabin-admin, ... (a role without the javabin- prefix gets one). Existing
profiles are never changed or deleted; the file is backed up to
<config>.bak-<timestamp> before writing.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAwsSetup(cmd.Context(), defaultAwsDeps(), awsSetupForce, awsSetupNoLogin, awsSetupDryRun)
	},
}

func runAwsSetup(ctx context.Context, d awsDeps, force, noLogin, dryRun bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	orig, err := readConfig(d.configPath)
	if err != nil {
		return err
	}

	// 1. The sso-session block must be on disk before aws sso login can use it.
	withSession, r, err := awsconfig.Apply(orig, nil, force)
	if err != nil {
		return err
	}
	backedUp := false
	if r.SessionChanged {
		if dryRun {
			fmt.Fprintf(d.out, "Would add/update [%s] in %s\n", awsconfig.SessionSection, d.configPath)
		} else {
			bak, err := awsconfig.WriteFile(d.configPath, withSession, true, d.now())
			if err != nil {
				return err
			}
			backedUp = true
			fmt.Fprintf(d.out, "Wrote [%s] to %s\n", awsconfig.SessionSection, d.configPath)
			if bak != "" {
				fmt.Fprintf(d.out, "Backup: %s\n", bak)
			}
		}
	}

	// 2. Sign in (writes only the AWS CLI's token cache).
	if !noLogin && !dryRun {
		if err := d.login(); err != nil {
			return err
		}
	}

	// 3. Roles this user actually has.
	tok, err := awsconfig.ReadToken(d.cacheDir, awsconfig.SessionName, d.now())
	if err != nil {
		if dryRun && errors.Is(err, awsconfig.ErrNoToken) {
			fmt.Fprintf(d.out, "Not signed in (%v); profiles are listed after 'aws sso login --sso-session %s'.\n", err, awsconfig.SessionName)
			if r.SessionChanged {
				printDiff(d.out, orig, withSession)
			}
			return nil
		}
		return fmt.Errorf("%w — run: aws sso login --sso-session %s", err, awsconfig.SessionName)
	}
	roles, err := d.roles.ListRoles(ctx, tok.AccessToken, awsconfig.AccountID)
	if err != nil {
		return fmt.Errorf("list your roles in account %s: %w", awsconfig.AccountID, err)
	}
	if len(roles) == 0 {
		return fmt.Errorf("you have no roles in account %s — ask a javaBin admin to add you to your team", awsconfig.AccountID)
	}

	// 4. Profiles.
	final, pr, err := awsconfig.Apply(withSession, roles, force)
	if err != nil {
		return err
	}
	if dryRun {
		if final == orig {
			fmt.Fprintln(d.out, "No changes.")
		} else {
			printDiff(d.out, orig, final)
		}
	} else if final != withSession {
		bak, err := awsconfig.WriteFile(d.configPath, final, !backedUp, d.now())
		if err != nil {
			return err
		}
		if bak != "" {
			fmt.Fprintf(d.out, "Backup: %s\n", bak)
		}
	}
	printSetupSummary(d.out, roles, pr, dryRun)
	return nil
}

func readConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// printDiff shows the lines added (new content is always appended or
// inserted, never removed).
func printDiff(w io.Writer, before, after string) {
	old := map[string]int{}
	for _, l := range strings.Split(before, "\n") {
		old[l]++
	}
	fmt.Fprintln(w, "Changes:")
	for _, l := range strings.Split(strings.TrimRight(after, "\n"), "\n") {
		if old[l] > 0 {
			old[l]--
			continue
		}
		fmt.Fprintln(w, "+ "+l)
	}
}

func printSetupSummary(w io.Writer, roles []string, r *awsconfig.Result, dryRun bool) {
	verb := "Added"
	if dryRun {
		verb = "Would add"
	}
	if len(r.Added) > 0 {
		fmt.Fprintf(w, "%s profiles: %s\n", verb, strings.Join(r.Added, ", "))
	}
	if len(r.Unchanged) > 0 {
		fmt.Fprintf(w, "Already up to date: %s\n", strings.Join(r.Unchanged, ", "))
	}
	for _, p := range r.Conflicts {
		fmt.Fprintf(w, "Left alone: [profile %s] exists with different settings — fix or remove it yourself if it should point at role %s\n", p, p)
	}
	sort.Strings(roles)
	var teams []string
	for _, role := range roles {
		profile := awsconfig.ProfileName(role)
		if other, ok := r.Covered[role]; ok {
			profile = other
			fmt.Fprintf(w, "Role %s already has profile %s; not adding another\n", role, other)
		}
		if team, ok := strings.CutPrefix(role, "javabin-team-"); ok {
			teams = append(teams, fmt.Sprintf("  team %-20s --profile %s", team, profile))
		}
	}
	if len(teams) > 0 {
		fmt.Fprintln(w, "\nProfiles per team:")
		for _, t := range teams {
			fmt.Fprintln(w, t)
		}
		fmt.Fprintln(w, "\n`javabin secret ...` picks the team profile from app.yaml automatically.")
	}
	fmt.Fprintf(w, "Sessions last about 8 hours; renew with: aws sso login --sso-session %s\n", awsconfig.SessionName)
}

func ssoLogin() error {
	bin, err := exec.LookPath("aws")
	if err != nil {
		return errors.New("the AWS CLI (v2) is not installed. Install it, then rerun javabin aws setup:\n" +
			"  macOS:  brew install awscli\n" +
			"  other:  https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html")
	}
	c := exec.Command(bin, "sso", "login", "--sso-session", awsconfig.SessionName)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("aws sso login: %w", err)
	}
	return nil
}

var awsDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the javaBin SSO config, token and which profile `secret` would use",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAwsDoctor(defaultAwsDeps(), awsDoctorYaml)
	},
}

func runAwsDoctor(d awsDeps, appYaml string) error {
	ok := true
	report := func(good bool, format string, a ...any) {
		mark := "ok  "
		if !good {
			mark, ok = "FAIL", false
		}
		fmt.Fprintf(d.out, "[%s] %s\n", mark, fmt.Sprintf(format, a...))
	}
	data, err := readConfig(d.configPath)
	if err != nil {
		return err
	}
	f := awsconfig.Parse(data)
	sess := f.Values(awsconfig.SessionSection)
	report(sess != nil && sess["sso_start_url"] == awsconfig.StartURL, "%s has [%s] for %s", d.configPath, awsconfig.SessionSection, awsconfig.StartURL)
	if tok, err := awsconfig.ReadToken(d.cacheDir, awsconfig.SessionName, d.now()); err != nil {
		report(false, "SSO token: %v (run: aws sso login --sso-session %s)", err, awsconfig.SessionName)
	} else {
		report(true, "SSO token valid until %s", tok.ExpiresAt.Local().Format("15:04 Mon 2 Jan"))
	}
	if app, err := loadAppYaml(appYaml); err == nil {
		p := jbaws.FindTeamProfile(d.configPath, app.Team)
		report(p != "", "%s (team %s): javabin secret would use profile %q", appYaml, app.Team, p)
	} else {
		fmt.Fprintf(d.out, "[skip] no %s here — team profile not checked\n", appYaml)
	}
	if !ok {
		return errors.New("problems found — run: javabin aws setup")
	}
	return nil
}

func init() {
	awsSetupCmd.Flags().BoolVar(&awsSetupForce, "force", false, "replace the settings of an existing [sso-session javabin] that points elsewhere")
	awsSetupCmd.Flags().BoolVar(&awsSetupNoLogin, "no-login", false, "skip aws sso login (use the cached token)")
	awsSetupCmd.Flags().BoolVar(&awsSetupDryRun, "dry-run", false, "print the changes without writing or signing in")
	awsDoctorCmd.Flags().StringVar(&awsDoctorYaml, "app-yaml", "app.yaml", "path to app.yaml")
	awsCmd.AddCommand(awsSetupCmd, awsDoctorCmd)
	rootCmd.AddCommand(awsCmd)
}
