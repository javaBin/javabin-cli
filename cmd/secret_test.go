package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAppYaml(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSecretPathMatchesServiceSecretModule(t *testing.T) {
	app, err := loadAppYaml(writeAppYaml(t, "name: cupcake-backend\nteam: cupcake\nresources:\n  secrets:\n    - name: sp_user\n      env: SP_USER\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := secretPath(app, "sp_user"); got != "/javabin/apps/cupcake/cupcake-backend/sp_user" {
		t.Errorf("secretPath = %s", got)
	}
	if !app.declares("sp_user") || app.declares("sp_password") {
		t.Error("declares() should only accept secrets listed in app.yaml")
	}
}

func TestLoadAppYamlRequiresNameAndTeam(t *testing.T) {
	if _, err := loadAppYaml(writeAppYaml(t, "name: x\n")); err == nil {
		t.Error("expected an error without team")
	}
}

func TestReadSecretValueFromStdinTrimsNewline(t *testing.T) {
	v, err := readSecretValue(strings.NewReader("s3cret\n"), true, "sp_user")
	if err != nil || v != "s3cret" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestReadSecretValueRejectsEmptyAndPlaceholder(t *testing.T) {
	for _, in := range []string{"\n", "PLACEHOLDER\n"} {
		if _, err := readSecretValue(strings.NewReader(in), true, "sp_user"); err == nil {
			t.Errorf("expected rejection for %q", in)
		}
	}
}
