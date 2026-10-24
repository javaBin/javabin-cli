package update

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "0.2.0", true},
		{"v0.2.1", "v0.2.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.2.0", "v0.10.0", false},
		{"v0.3.0", "0.3.0-rc1", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNoticeChecksAtMostDaily(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	calls := 0
	fetch := func() (string, error) { calls++; return "v0.3.0", nil }
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	msg := Notice("0.2.0", fetch, path, now)
	if !strings.Contains(msg, "0.3.0 is available") || !strings.Contains(msg, "brew upgrade javabin") {
		t.Fatalf("unexpected notice %q", msg)
	}
	Notice("0.2.0", fetch, path, now.Add(time.Hour)) // cached
	if calls != 1 {
		t.Errorf("fetched %d times within a day, want 1", calls)
	}
	Notice("0.2.0", fetch, path, now.Add(25*time.Hour))
	if calls != 2 {
		t.Errorf("fetched %d times after a day, want 2", calls)
	}
}

func TestNoticeQuietWhenCurrentDevOrFailing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	now := time.Now()
	if msg := Notice("0.3.0", func() (string, error) { return "v0.3.0", nil }, path, now); msg != "" {
		t.Errorf("up to date but got %q", msg)
	}
	if msg := Notice("dev", func() (string, error) { return "v9.9.9", nil }, path, now); msg != "" {
		t.Errorf("dev build but got %q", msg)
	}
	if msg := Notice("0.1.0", func() (string, error) { return "", errors.New("offline") }, filepath.Join(t.TempDir(), "x.json"), now); msg != "" {
		t.Errorf("fetch failed but got %q", msg)
	}
}

func TestFailedCheckIsNotRetriedOnEveryCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	calls := 0
	fetch := func() (string, error) { calls++; return "", errors.New("offline") }
	now := time.Now()
	Notice("0.2.0", fetch, path, now)
	Notice("0.2.0", fetch, path, now.Add(time.Minute))
	if calls != 1 {
		t.Errorf("fetched %d times while offline within a day, want 1", calls)
	}
}
