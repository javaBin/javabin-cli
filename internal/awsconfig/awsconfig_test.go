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
	orig := "[profile cupcake]\nsso_session = javabin\nsso_account_id = 553637109631\nsso_role_name = javabin-team-cupcake\n"
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

func TestApplyOtherSessionNotCovered(t *testing.T) {
	orig := "[profile cupcake]\nsso_session = jb\nsso_account_id = 553637109631\nsso_role_name = javabin-team-cupcake\n"
	got, r, _ := Apply(orig, roles, false)
	if !strings.Contains(got, "[profile javabin-team-cupcake]") || r.Covered["javabin-team-cupcake"] != "" {
		t.Errorf("profile on another session must not count as covered: %+v\n%s", r, got)
	}
	if refs := r.OtherSession["javabin-team-cupcake"]; len(refs) != 1 || refs[0] != (ProfileRef{"cupcake", "jb"}) {
		t.Errorf("OtherSession %v", r.OtherSession)
	}
	if amb := r.Ambiguous["javabin-team-cupcake"]; len(amb) != 2 {
		t.Errorf("Ambiguous %v", r.Ambiguous)
	}
	if !strings.HasPrefix(got, orig) {
		t.Error("existing profile changed")
	}
}

func TestApplyAmbiguousExistingProfiles(t *testing.T) {
	orig := "[profile a]\nsso_session = javabin\nsso_role_name = javabin-team-cupcake\n[profile b]\nsso_session = javabin\nsso_role_name = javabin-team-cupcake\n"
	_, r, _ := Apply(orig, []string{"javabin-team-cupcake"}, false)
	if r.Covered["javabin-team-cupcake"] != "a" || len(r.Ambiguous["javabin-team-cupcake"]) != 2 || len(r.Added) != 0 {
		t.Errorf("%+v", r)
	}
}

func TestSetSkipsNestedBlock(t *testing.T) {
	f := Parse("[sso-session javabin]\nsso_start_url = " + StartURL + "\ns3 =\n  max_concurrent_requests = 20\n\n[profile x]\n")
	f.Set(SessionSection, "sso_region", "eu-central-1")
	want := "[sso-session javabin]\nsso_start_url = " + StartURL + "\ns3 =\n  max_concurrent_requests = 20\nsso_region = eu-central-1\n\n[profile x]\n"
	if got := f.String(); got != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestParseHeaderWithComment(t *testing.T) {
	f := Parse("[profile a] # note\nregion = x\n[profile  b] ; other\nregion = y\n[not a header] junk\n")
	if f.Values("profile a")["region"] != "x" || f.Values("profile b")["region"] != "y" {
		t.Errorf("sections %v", f.SectionNames())
	}
	if f.Has("not a header") {
		t.Error("trailing junk must not make a header")
	}
}

func TestWriteFileFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "dotfiles")
	_ = os.MkdirAll(realDir, 0o700)
	target := filepath.Join(realDir, "aws-config")
	_ = os.WriteFile(target, []byte("old\n"), 0o600)
	link := filepath.Join(dir, "config")
	if err := os.Symlink("dotfiles/aws-config", link); err != nil {
		t.Fatal(err)
	}
	bak, err := WriteFile(link, "new\n", true, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced by a regular file")
	}
	if got, _ := os.ReadFile(target); string(got) != "new\n" {
		t.Errorf("target = %q", got)
	}
	realTargetDir, _ := filepath.EvalSymlinks(realDir)
	if old, _ := os.ReadFile(bak); string(old) != "old\n" || filepath.Dir(bak) != realTargetDir {
		t.Errorf("backup %s = %q", bak, old)
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, ".config-*")); len(tmps) != 0 {
		t.Errorf("temp files left: %v", tmps)
	}
	// dangling link: target gets created, link kept
	dangling := filepath.Join(dir, "config2")
	_ = os.Symlink(filepath.Join(realDir, "new-config"), dangling)
	if _, err := WriteFile(dangling, "x\n", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(realDir, "new-config")); string(got) != "x\n" {
		t.Errorf("dangling target = %q", got)
	}
}
