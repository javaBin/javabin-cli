// Package update tells users when a newer javabin release exists.
//
// At most once a day it asks GitHub for the latest release (cached in
// ~/.javabin/update-check.json) and prints a one-line notice on stderr.
// It never blocks for long, never fails a command, and stays quiet for dev
// builds, non-terminal output and JAVABIN_NO_UPDATE_CHECK=1.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/javaBin/javabin-cli/internal/config"
)

const (
	latestURL   = "https://api.github.com/repos/javaBin/javabin-cli/releases/latest"
	checkEvery  = 24 * time.Hour
	httpTimeout = 2 * time.Second
	cacheFile   = "update-check.json"
)

type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// Notice returns the message to show, or "" when current is up to date (or
// the check is skipped or fails).
func Notice(current string, fetch func() (string, error), cachePath string, now time.Time) string {
	if current == "" || current == "dev" {
		return ""
	}
	c := readCache(cachePath)
	if now.Sub(c.CheckedAt) > checkEvery {
		if latest, err := fetch(); err == nil && latest != "" {
			c = cache{CheckedAt: now, Latest: latest}
			writeCache(cachePath, c)
		}
	}
	if c.Latest == "" || !Newer(c.Latest, current) {
		return ""
	}
	return fmt.Sprintf("javabin %s is available (you have %s). Update: brew upgrade javabin  (or: go install github.com/javaBin/javabin-cli@latest)",
		strings.TrimPrefix(c.Latest, "v"), strings.TrimPrefix(current, "v"))
}

// PrintIfOutdated prints the notice on stderr when it applies.
func PrintIfOutdated(current string) {
	if os.Getenv("JAVABIN_NO_UPDATE_CHECK") != "" || !isTerminal(os.Stderr) {
		return
	}
	dir, err := config.Dir()
	if err != nil {
		return
	}
	if msg := Notice(current, fetchLatest, filepath.Join(dir, cacheFile), time.Now()); msg != "" {
		fmt.Fprintln(os.Stderr, "\n"+msg)
	}
}

func fetchLatest() (string, error) {
	client := http.Client{Timeout: httpTimeout}
	req, _ := http.NewRequest("GET", latestURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	return body.TagName, nil
}

// Newer reports whether version a is newer than b ("v1.2.3" or "1.2.3").
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "-") // ignore pre-release suffixes
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func readCache(path string) cache {
	var c cache
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func writeCache(path string, c cache) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if data, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
