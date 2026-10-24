// Package awsconfig reads and edits the AWS CLI config file (~/.aws/config)
// without disturbing content it does not own: unrelated sections, comments,
// blank lines and ordering are written back byte-for-byte.
package awsconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultPath is AWS_CONFIG_FILE, else ~/.aws/config.
func DefaultPath() string {
	if p := os.Getenv("AWS_CONFIG_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", "config")
}

// KV is one key = value setting, kept ordered so new sections render stably.
type KV struct{ Key, Value string }

type section struct {
	name  string   // header content, e.g. "profile foo", "sso-session javabin", "default"
	lines []string // raw lines including the header (empty name: preamble, no header)
}

// File is a parsed config file. Only the sections touched through Set /
// AddSection change when rendered.
type File struct {
	sections []*section
	trailNL  bool
}

// Parse splits data into sections. It never fails: lines it does not
// understand are kept verbatim.
func Parse(data string) *File {
	f := &File{trailNL: data == "" || strings.HasSuffix(data, "\n")}
	cur := &section{}
	f.sections = append(f.sections, cur)
	if data == "" {
		return f
	}
	for _, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			cur = &section{name: strings.Join(strings.Fields(t[1:len(t)-1]), " "), lines: []string{line}}
			f.sections = append(f.sections, cur)
			continue
		}
		cur.lines = append(cur.lines, line)
	}
	return f
}

// String renders the file.
func (f *File) String() string {
	var all []string
	for _, s := range f.sections {
		all = append(all, s.lines...)
	}
	out := strings.Join(all, "\n")
	if out != "" && f.trailNL {
		out += "\n"
	}
	return out
}

func (f *File) find(name string) *section {
	for _, s := range f.sections[1:] {
		if s.name == name {
			return s
		}
	}
	return nil
}

// Has reports whether a section with this header exists.
func (f *File) Has(name string) bool { return f.find(name) != nil }

// Values returns a section's top-level key/values (nested sub-properties and
// comments are ignored), or nil if the section does not exist.
func (f *File) Values(name string) map[string]string {
	s := f.find(name)
	if s == nil {
		return nil
	}
	vals := map[string]string{}
	for _, line := range s.lines[1:] {
		if k, v, ok := parseKV(line); ok {
			vals[k] = v
		}
	}
	return vals
}

// SectionNames lists section headers in file order.
func (f *File) SectionNames() []string {
	var names []string
	for _, s := range f.sections[1:] {
		names = append(names, s.name)
	}
	return names
}

func parseKV(line string) (string, string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' { // nested sub-property
		return "", "", false
	}
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' || t[0] == ';' {
		return "", "", false
	}
	k, v, ok := strings.Cut(t, "=")
	if !ok {
		return "", "", false
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	if i := strings.Index(v, " ;"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(k), strings.TrimSpace(v), true
}

// Set updates key in an existing section in place, or appends it after the
// section's last setting. It reports whether anything changed.
func (f *File) Set(name, key, value string) bool {
	s := f.find(name)
	if s == nil {
		return false
	}
	last := 0
	for i, line := range s.lines[1:] {
		k, v, ok := parseKV(line)
		if !ok {
			continue
		}
		last = i + 1
		if k == key {
			if v == value {
				return false
			}
			s.lines[i+1] = key + " = " + value
			return true
		}
	}
	s.lines = append(s.lines[:last+1], append([]string{key + " = " + value}, s.lines[last+1:]...)...)
	return true
}

// AddSection appends a new section at the end of the file, separated by a
// blank line.
func (f *File) AddSection(name string, kvs []KV) {
	lines := []string{"[" + name + "]"}
	for _, kv := range kvs {
		lines = append(lines, kv.Key+" = "+kv.Value)
	}
	if last := f.sections[len(f.sections)-1]; len(last.lines) > 0 && strings.TrimSpace(last.lines[len(last.lines)-1]) != "" {
		lines = append([]string{""}, lines...)
	}
	f.sections = append(f.sections, &section{name: name, lines: lines})
	f.trailNL = true
}

// WriteFile writes data atomically (temp file + rename) with mode 0600. With
// backup it first copies an existing file to <path>.bak-<timestamp> and
// returns that path ("" when there was nothing to back up).
func WriteFile(path, data string, backup bool, now time.Time) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	bak := ""
	if old, err := os.ReadFile(path); err == nil && backup {
		bak = path + ".bak-" + now.Format("20060102-150405")
		if err := os.WriteFile(bak, old, 0o600); err != nil {
			return "", fmt.Errorf("backup %s: %w", bak, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return bak, nil
}
