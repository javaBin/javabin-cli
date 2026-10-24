package awsconfig

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var roles = []string{"javabin-team-cupcake", "javabin-developer"}

func TestApplyEmptyFile(t *testing.T) {
	got, r, err := Apply("", roles, false)
	if err != nil {
		t.Fatal(err)
	}
	want := `[sso-session javabin]
sso_start_url = https://identitycenter.amazonaws.com/ssoins-6987654c95c9a069
sso_region = eu-central-1
sso_registration_scopes = sso:account:access

[profile javabin-developer]
sso_session = javabin
sso_account_id = 553637109631
sso_role_name = javabin-developer
region = eu-central-1

[profile javabin-team-cupcake]
sso_session = javabin
sso_account_id = 553637109631
sso_role_name = javabin-team-cupcake
region = eu-central-1
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if !r.SessionChanged || len(r.Added) != 2 {
		t.Errorf("result %+v", r)
	}
}

func TestApplyKeepsOtherContent(t *testing.T) {
	orig := `# my settings
[default]
region = us-east-1 # comment

[profile work]
sso_start_url = https://example.awsapps.com/start
sso_role_name = Admin
s3 =
  max_concurrent_requests = 20
`
	got, r, err := Apply(orig, roles, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, orig) {
		t.Errorf("existing content changed:\n%s", got)
	}
	if len(r.Added) != 2 || r.Changed() == false {
		t.Errorf("result %+v", r)
	}
	// unrelated profiles still parse the same
	if v := Parse(got).Values("profile work"); v["sso_role_name"] != "Admin" {
		t.Errorf("work profile: %v", v)
	}
}

func TestApplyIdempotent(t *testing.T) {
	first, _, _ := Apply("[default]\nregion = eu-north-1\n", roles, false)
	second, r, err := Apply(first, roles, false)
	if err != nil {
		t.Fatal(err)
	}
	if second != first || r.Changed() || len(r.Unchanged) != 2 {
		t.Errorf("second run changed things: %+v\n%s", r, second)
	}
}

func TestApplySessionConflict(t *testing.T) {
	orig := "[sso-session javabin]\nsso_start_url = https://other.awsapps.com/start\nsso_region = us-east-1\n"
	got, _, err := Apply(orig, roles, false)
	var conflict *ErrSessionConflict
	if !errors.As(err, &conflict) || got != orig {
		t.Fatalf("want conflict and unchanged file, got err=%v", err)
	}
	got, r, err := Apply(orig, roles, true)
	if err != nil || !r.SessionChanged {
		t.Fatalf("force: err=%v r=%+v", err, r)
	}
	v := Parse(got).Values(SessionSection)
	if v["sso_start_url"] != StartURL || v["sso_region"] != SSORegion || v["sso_registration_scopes"] != "sso:account:access" {
		t.Errorf("forced session: %v", v)
	}
	if strings.Count(got, "[sso-session javabin]") != 1 {
		t.Errorf("duplicated session block:\n%s", got)
	}
}

func TestApplySessionCompletesMissingKeys(t *testing.T) {
	orig := "[sso-session javabin]\nsso_start_url = " + StartURL + "\nsso_region = eu-central-1\n\n[profile x]\nregion = a\n"
	got, r, err := Apply(orig, nil, false)
	if err != nil || !r.SessionChanged {
		t.Fatalf("err=%v r=%+v", err, r)
	}
	want := "[sso-session javabin]\nsso_start_url = " + StartURL + "\nsso_region = eu-central-1\nsso_registration_scopes = sso:account:access\n\n[profile x]\nregion = a\n"
	if got != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestApplyProfileConflictLeftAlone(t *testing.T) {
	orig := "[profile javabin-developer]\nsso_role_name = javabin-developer\nsso_account_id = 111111111111\n"
	got, r, err := Apply(orig, roles, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0] != "javabin-developer" {
		t.Errorf("conflicts %v", r.Conflicts)
	}
	if v := Parse(got).Values("profile javabin-developer"); v["sso_account_id"] != "111111111111" || v["sso_session"] != "" {
		t.Errorf("conflicting profile modified: %v", v)
	}
}

func TestApplyRoleAlreadyCoveredByOtherProfile(t *testing.T) {
	orig := "[profile cupcake]\nsso_account_id = 553637109631\nsso_role_name = javabin-team-cupcake\n"
	got, r, _ := Apply(orig, roles, false)
	if r.Covered["javabin-team-cupcake"] != "cupcake" || strings.Contains(got, "[profile javabin-team-cupcake]") {
		t.Errorf("should not add a second profile for the role: %+v\n%s", r, got)
	}
}

func TestProfileName(t *testing.T) {
	for role, want := range map[string]string{"javabin-team-cupcake": "javabin-team-cupcake", "javabin-admin": "javabin-admin", "ReadOnly": "javabin-ReadOnly"} {
		if got := ProfileName(role); got != want {
			t.Errorf("%s: got %s", role, got)
		}
	}
}

func TestWriteFileBackupAndMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "aws", "config")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if bak, err := WriteFile(p, "a\n", true, now); err != nil || bak != "" {
		t.Fatalf("new file: bak=%q err=%v", bak, err)
	}
	bak, err := WriteFile(p, "b\n", true, now)
	if err != nil {
		t.Fatal(err)
	}
	if old, _ := os.ReadFile(bak); string(old) != "a\n" || !strings.HasSuffix(bak, "config.bak-20261024-120000") {
		t.Errorf("backup %s = %q", bak, old)
	}
	st, _ := os.Stat(p)
	if cur, _ := os.ReadFile(p); string(cur) != "b\n" || st.Mode().Perm() != 0o600 {
		t.Errorf("content %q mode %v", cur, st.Mode())
	}
}

func writeToken(t *testing.T, dir, expires string) {
	t.Helper()
	sum := sha1.Sum([]byte("javabin"))
	body := `{"startUrl":"x","region":"eu-central-1","accessToken":"tok123","expiresAt":"` + expires + `"}`
	if err := os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadToken(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if _, err := ReadToken(dir, "javabin", now); !errors.Is(err, ErrNoToken) {
		t.Errorf("missing: %v", err)
	}
	writeToken(t, dir, "2026-10-24T20:00:00Z")
	tok, err := ReadToken(dir, "javabin", now)
	if err != nil || tok.AccessToken != "tok123" {
		t.Fatalf("valid: %v %+v", err, tok)
	}
	writeToken(t, dir, "2026-10-24T11:00:00UTC")
	if _, err := ReadToken(dir, "javabin", now); !errors.Is(err, ErrNoToken) {
		t.Errorf("expired: %v", err)
	}
}

type fakeLister struct{ token, account string }

func (f *fakeLister) ListRoles(_ context.Context, token, account string) ([]string, error) {
	f.token, f.account = token, account
	return roles, nil
}

func TestRoleListerInterface(t *testing.T) {
	var l RoleLister = &fakeLister{}
	got, _ := l.ListRoles(context.Background(), "tok", AccountID)
	if len(got) != 2 {
		t.Error(got)
	}
	var _ RoleLister = SSORoleLister{}
}
