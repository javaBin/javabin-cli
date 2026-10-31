package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddRepoToTeam(t *testing.T) {
	var method, path string
	var body map[string]string
	status := http.StatusNoContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	if err := AddRepoToTeam("tok", "video", "recorder"); err != nil {
		t.Fatal(err)
	}
	if method != "PUT" || path != "/orgs/javaBin/teams/video/repos/javaBin/recorder" || body["permission"] != "push" {
		t.Errorf("request = %s %s %v", method, path, body)
	}

	for _, s := range []int{http.StatusForbidden, http.StatusNotFound} {
		status = s
		if err := AddRepoToTeam("tok", "video", "recorder"); !errors.Is(err, ErrNoTeamAccess) {
			t.Errorf("status %d: err = %v, want ErrNoTeamAccess", s, err)
		}
	}
	status = http.StatusInternalServerError
	if err := AddRepoToTeam("tok", "video", "recorder"); err == nil || errors.Is(err, ErrNoTeamAccess) {
		t.Errorf("status 500: err = %v", err)
	}
}
