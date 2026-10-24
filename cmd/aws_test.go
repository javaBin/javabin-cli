package cmd

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jbaws "github.com/javaBin/javabin-cli/internal/aws"
)

type fakeRoles struct {
	roles []string
	token string
}

func (f *fakeRoles) ListRoles(_ context.Context, token, _ string) ([]string, error) {
	f.token = token
	return f.roles, nil
}

func testDeps(t *testing.T) (awsDeps, *bytes.Buffer, *int) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	_ = os.MkdirAll(cache, 0o700)
	logins := 0
	out := &bytes.Buffer{}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	return awsDeps{
		configPath: filepath.Join(dir, ".aws", "config"),
		cacheDir:   cache,
		lookAws:    func() error { return nil },
		login: func() error {
			logins++
			sum := sha1.Sum([]byte("javabin"))
			return os.WriteFile(filepath.Join(cache, hex.EncodeToString(sum[:])+".json"),
				[]byte(`{"accessToken":"tok","expiresAt":"2026-10-24T20:00:00Z"}`), 0o600)
		},
		roles: &fakeRoles{roles: []string{"javabin-team-cupcake", "javabin-developer"}},
		now:   func() time.Time { return now },
		out:   out,
	}, out, &logins
}

func TestAwsSetupEndToEnd(t *testing.T) {
	d, out, logins := testDeps(t)
	_ = os.MkdirAll(filepath.Dir(d.configPath), 0o700)
	_ = os.WriteFile(d.configPath, []byte("[profile mine]\nregion = us-east-1\n"), 0o644)

	if err := runAwsSetup(context.Background(), d, false, false, false); err != nil {
		t.Fatal(err)
	}
	if *logins != 1 || d.roles.(*fakeRoles).token != "tok" {
		t.Errorf("logins=%d token=%q", *logins, d.roles.(*fakeRoles).token)
	}
	data, _ := os.ReadFile(d.configPath)
	if !strings.HasPrefix(string(data), "[profile mine]\nregion = us-east-1\n") {
		t.Errorf("existing profile changed:\n%s", data)
	}
	if got := jbaws.FindTeamProfile(d.configPath, "cupcake"); got != "javabin-team-cupcake" {
		t.Errorf("secret would pick %q", got)
	}
	baks, _ := filepath.Glob(d.configPath + ".bak-*")
	if len(baks) != 1 {
		t.Errorf("want exactly one backup, got %v", baks)
	}
	if st, _ := os.Stat(d.configPath); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	if !strings.Contains(out.String(), "team cupcake") {
		t.Errorf("summary:\n%s", out)
	}

	// second run: nothing to write
	before := string(data)
	if err := runAwsSetup(context.Background(), d, false, true, false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(d.configPath)
	if string(after) != before {
		t.Error("second run changed the file")
	}
}

func TestAwsSetupDryRunWritesNothing(t *testing.T) {
	d, out, logins := testDeps(t)
	if err := runAwsSetup(context.Background(), d, false, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.configPath); !os.IsNotExist(err) || *logins != 0 {
		t.Errorf("dry run wrote config or logged in (logins=%d)", *logins)
	}
	if !strings.Contains(out.String(), "+ [sso-session javabin]") {
		t.Errorf("output:\n%s", out)
	}
}

func TestAwsSetupSessionConflictStops(t *testing.T) {
	d, _, logins := testDeps(t)
	_ = os.MkdirAll(filepath.Dir(d.configPath), 0o700)
	orig := "[sso-session javabin]\nsso_start_url = https://other.awsapps.com/start\n"
	_ = os.WriteFile(d.configPath, []byte(orig), 0o600)
	if err := runAwsSetup(context.Background(), d, false, false, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want conflict error, got %v", err)
	}
	if data, _ := os.ReadFile(d.configPath); string(data) != orig || *logins != 0 {
		t.Error("conflict must not write or log in")
	}
}

func TestAwsSetupNoAwsCLIWritesNothing(t *testing.T) {
	d, _, logins := testDeps(t)
	d.lookAws = func() error { return errors.New("the AWS CLI (v2) is not installed") }
	if err := runAwsSetup(context.Background(), d, false, false, false); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want install error, got %v", err)
	}
	if _, err := os.Stat(d.configPath); !os.IsNotExist(err) || *logins != 0 {
		t.Error("must not write config or log in without the AWS CLI")
	}
	// --no-login does not need the CLI
	if err := runAwsSetup(context.Background(), d, false, true, false); err == nil || strings.Contains(err.Error(), "not installed") {
		t.Fatalf("--no-login should fail only on the missing token, got %v", err)
	}
}

func TestAwsSetupWarnsAboutOtherSessionProfile(t *testing.T) {
	d, out, _ := testDeps(t)
	_ = os.MkdirAll(filepath.Dir(d.configPath), 0o700)
	_ = os.WriteFile(d.configPath, []byte("[profile cupcake]\nsso_session = jb\nsso_account_id = 553637109631\nsso_role_name = javabin-team-cupcake\n"), 0o600)
	if err := runAwsSetup(context.Background(), d, false, false, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(d.configPath)
	if !strings.Contains(string(data), "[profile javabin-team-cupcake]") {
		t.Errorf("javabin-session profile not added:\n%s", data)
	}
	o := out.String()
	for _, want := range []string{"[profile cupcake] also uses role javabin-team-cupcake but with sso_session = jb", "cannot pick one for team cupcake", "--profile javabin-team-cupcake"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
	if strings.Contains(o, "picks the team profile from app.yaml automatically") {
		t.Error("must not promise auto-discovery when it is off")
	}
}

func TestAwsSetupConflictNamesRole(t *testing.T) {
	d, out, _ := testDeps(t)
	d.roles = &fakeRoles{roles: []string{"ReadOnly"}}
	_ = os.MkdirAll(filepath.Dir(d.configPath), 0o700)
	_ = os.WriteFile(d.configPath, []byte("[profile javabin-ReadOnly]\nregion = us-east-1\n"), 0o600)
	if err := runAwsSetup(context.Background(), d, false, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[profile javabin-ReadOnly] exists with different settings — fix or remove it yourself if it should point at role ReadOnly") {
		t.Errorf("output:\n%s", out)
	}
}

func TestAwsDoctorToleratesTrailingSlash(t *testing.T) {
	d, out, _ := testDeps(t)
	_ = os.MkdirAll(filepath.Dir(d.configPath), 0o700)
	_ = os.WriteFile(d.configPath, []byte("[sso-session javabin]\nsso_start_url = https://identitycenter.amazonaws.com/ssoins-6987654c95c9a069/#\n"), 0o600)
	_ = d.login() // writes a valid token
	if err := runAwsDoctor(d, filepath.Join(t.TempDir(), "app.yaml")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
