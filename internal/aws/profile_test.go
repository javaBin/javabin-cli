package aws

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindTeamProfile(t *testing.T) {
	cfg := `[default]
region = eu-central-1

[profile javabin]
sso_account_id = 553637109631
sso_role_name = javabin-admin

[profile cupcake]
sso_session = javabin
sso_account_id = 553637109631
sso_role_name = javabin-team-cupcake

[profile other-account]
sso_account_id = 111111111111
sso_role_name = javabin-team-jzwatch
`
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := FindTeamProfile(p, "cupcake"); got != "cupcake" {
		t.Errorf("cupcake: got %q", got)
	}
	if got := FindTeamProfile(p, "jzwatch"); got != "" {
		t.Errorf("jzwatch in another account must not match, got %q", got)
	}
	if got := FindTeamProfile(filepath.Join(t.TempDir(), "missing"), "cupcake"); got != "" {
		t.Errorf("missing file: got %q", got)
	}
}

func TestFindTeamProfileAmbiguous(t *testing.T) {
	cfg := "[profile a]\nsso_role_name = javabin-team-cupcake\n[profile b]\nsso_role_name = javabin-team-cupcake\n"
	p := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(p, []byte(cfg), 0o600)
	if got := FindTeamProfile(p, "cupcake"); got != "" {
		t.Errorf("two matches must not guess, got %q", got)
	}
}
