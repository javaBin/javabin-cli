package awsconfig

import (
	"fmt"
	"sort"
	"strings"
)

// javaBin Identity Center.
const (
	SessionName = "javabin"
	StartURL    = "https://identitycenter.amazonaws.com/ssoins-6987654c95c9a069"
	SSORegion   = "eu-central-1"
	AccountID   = "553637109631"
	Region      = "eu-central-1"
	scopes      = "sso:account:access"
)

// SessionSection is the header of the javaBin sso-session block.
const SessionSection = "sso-session " + SessionName

// ProfileName maps an SSO role to its profile name: javabin-* roles keep
// their name (javabin-team-cupcake -> javabin-team-cupcake, javabin-admin ->
// javabin-admin); any other role gets a javabin- prefix so every profile this
// command writes is recognisably javaBin's.
func ProfileName(role string) string {
	if strings.HasPrefix(role, "javabin-") {
		return role
	}
	return "javabin-" + role
}

// ErrSessionConflict means an sso-session javabin block points elsewhere.
type ErrSessionConflict struct{ Existing string }

func (e *ErrSessionConflict) Error() string {
	return fmt.Sprintf("[%s] already exists with sso_start_url = %q (expected %s); not overwriting — rerun with --force to replace its settings", SessionSection, e.Existing, StartURL)
}

// Result describes what EnsureSession/EnsureProfiles did to the file.
type Result struct {
	SessionChanged bool     // session block added or updated
	Added          []string // profiles added
	Unchanged      []string // profiles already as wanted
	Conflicts      []string // profiles that exist with different settings (left alone)
	// Covered maps a role to an existing profile (not named by ProfileName)
	// that already uses it; no second profile is added so `javabin secret`
	// keeps finding exactly one profile per team role.
	Covered map[string]string
}

// Changed reports whether the file content differs.
func (r *Result) Changed() bool { return r.SessionChanged || len(r.Added) > 0 }

// EnsureSession adds or completes the [sso-session javabin] block. A block
// with a different start URL is an *ErrSessionConflict unless force.
func EnsureSession(f *File, force bool) (bool, error) {
	want := []KV{{"sso_start_url", StartURL}, {"sso_region", SSORegion}, {"sso_registration_scopes", scopes}}
	vals := f.Values(SessionSection)
	if vals == nil {
		f.AddSection(SessionSection, want)
		return true, nil
	}
	if u := vals["sso_start_url"]; u != "" && strings.TrimRight(u, "/#") != StartURL && !force {
		return false, &ErrSessionConflict{Existing: u}
	}
	changed := false
	for _, kv := range want {
		if f.Set(SessionSection, kv.Key, kv.Value) {
			changed = true
		}
	}
	return changed, nil
}

// ProfileSettings is what a role's profile should contain.
func ProfileSettings(role string) []KV {
	return []KV{
		{"sso_session", SessionName},
		{"sso_account_id", AccountID},
		{"sso_role_name", role},
		{"region", Region},
	}
}

// EnsureProfiles adds a profile per role. Existing profiles are never edited:
// if one has the wanted settings it is unchanged, otherwise it is reported as
// a conflict.
func EnsureProfiles(f *File, roles []string, r *Result) {
	roles = append([]string(nil), roles...)
	sort.Strings(roles)
	for _, role := range roles {
		name := ProfileName(role)
		section := "profile " + name
		want := ProfileSettings(role)
		vals := f.Values(section)
		if vals == nil {
			if other := profileForRole(f, role); other != "" {
				if r.Covered == nil {
					r.Covered = map[string]string{}
				}
				r.Covered[role] = other
				continue
			}
			f.AddSection(section, want)
			r.Added = append(r.Added, name)
			continue
		}
		same := true
		for _, kv := range want {
			if vals[kv.Key] != kv.Value {
				same = false
			}
		}
		if same {
			r.Unchanged = append(r.Unchanged, name)
		} else {
			r.Conflicts = append(r.Conflicts, name)
		}
	}
}

// profileForRole finds an existing profile using role in the javaBin account.
func profileForRole(f *File, role string) string {
	for _, name := range f.SectionNames() {
		p, ok := strings.CutPrefix(name, "profile ")
		if !ok && name != "default" {
			continue
		}
		if !ok {
			p = name
		}
		v := f.Values(name)
		if v["sso_role_name"] == role && (v["sso_account_id"] == "" || v["sso_account_id"] == AccountID) {
			return p
		}
	}
	return ""
}

// Apply runs EnsureSession and EnsureProfiles on data and returns the new
// content.
func Apply(data string, roles []string, force bool) (string, *Result, error) {
	f := Parse(data)
	r := &Result{}
	changed, err := EnsureSession(f, force)
	if err != nil {
		return data, r, err
	}
	r.SessionChanged = changed
	EnsureProfiles(f, roles, r)
	return f.String(), r, nil
}
