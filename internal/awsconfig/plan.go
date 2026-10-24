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
	Conflicts      []string // roles whose ProfileName exists with different settings (left alone)
	// Covered maps a role to an existing profile (not named by ProfileName)
	// on sso_session javabin that already uses it; no second profile is added
	// so `javabin secret` keeps finding exactly one profile per team role.
	Covered map[string]string
	// OtherSession maps a role to existing profiles that use it through a
	// different sso_session (or a legacy inline sso_start_url). They are left
	// alone, but `aws sso login --sso-session javabin` does not refresh them.
	OtherSession map[string][]ProfileRef
	// Ambiguous maps a role to every profile using it when, after the
	// change, there is more than one: team-profile auto-discovery is off.
	Ambiguous map[string][]string
}

// ProfileRef names a profile and the sso_session it uses ("" = none).
type ProfileRef struct{ Profile, Session string }

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
	if u := vals["sso_start_url"]; u != "" && !SameStartURL(u) && !force {
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

// SameStartURL reports whether u is javaBin's start URL, ignoring a trailing
// "/" or "#".
func SameStartURL(u string) bool { return strings.TrimRight(u, "/#") == StartURL }

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
			covered := ""
			for _, ref := range profilesForRole(f, role) {
				if ref.Session == SessionName {
					if covered == "" {
						covered = ref.Profile
					}
					continue
				}
				if r.OtherSession == nil {
					r.OtherSession = map[string][]ProfileRef{}
				}
				r.OtherSession[role] = append(r.OtherSession[role], ref)
			}
			if covered != "" {
				if r.Covered == nil {
					r.Covered = map[string]string{}
				}
				r.Covered[role] = covered
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
			r.Conflicts = append(r.Conflicts, role)
		}
	}
	for _, role := range roles {
		if refs := profilesForRole(f, role); len(refs) > 1 {
			if r.Ambiguous == nil {
				r.Ambiguous = map[string][]string{}
			}
			for _, ref := range refs {
				r.Ambiguous[role] = append(r.Ambiguous[role], ref.Profile)
			}
		}
	}
}

// profilesForRole lists existing profiles using role in the javaBin account
// (the same rule FindTeamProfile uses for auto-discovery).
func profilesForRole(f *File, role string) []ProfileRef {
	var refs []ProfileRef
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
			refs = append(refs, ProfileRef{Profile: p, Session: v["sso_session"]})
		}
	}
	return refs
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
