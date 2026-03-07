package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

const (
	orgName       = "javaBin"
	registryOwner = "javaBin"
	registryRepo  = "registry"
	apiBase       = "https://api.github.com"
)

// GetToken returns a GitHub token from gh CLI or environment.
func GetToken() (string, error) {
	// Try gh CLI first
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		token := strings.TrimSpace(string(out))
		if token != "" {
			return token, nil
		}
	}

	// Try environment variable
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if token := strings.TrimSpace(getenv(env)); token != "" {
			return token, nil
		}
	}

	return "", fmt.Errorf("no GitHub token found — run 'gh auth login' or set GITHUB_TOKEN")
}

// CreateRegistrationPR creates a branch, commits a file, and opens a PR.
func CreateRegistrationPR(token, branch, filePath, content, title, body string) (string, error) {
	// Get default branch SHA
	mainSHA, err := getRef(token, "heads/main")
	if err != nil {
		return "", fmt.Errorf("get main ref: %w", err)
	}

	// Create branch
	if err := createRef(token, "refs/heads/"+branch, mainSHA); err != nil {
		return "", fmt.Errorf("create branch: %w", err)
	}

	// Create/update file on branch
	if err := createFile(token, branch, filePath, content, "Register "+strings.TrimPrefix(filePath, "apps/")); err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}

	// Open PR
	prURL, err := createPR(token, branch, title, body)
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}

	return prURL, nil
}

func getRef(token, ref string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/git/ref/%s", apiBase, registryOwner, registryRepo, ref)
	body, err := ghGet(token, url)
	if err != nil {
		return "", err
	}
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return result.Object.SHA, nil
}

func createRef(token, ref, sha string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/git/refs", apiBase, registryOwner, registryRepo)
	payload := map[string]string{"ref": ref, "sha": sha}
	_, err := ghPost(token, url, payload)
	return err
}

func createFile(token, branch, path, content, message string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", apiBase, registryOwner, registryRepo, path)
	payload := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	_, err := ghPut(token, url, payload)
	return err
}

func createPR(token, branch, title, body string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", apiBase, registryOwner, registryRepo)
	payload := map[string]string{
		"title": title,
		"head":  branch,
		"base":  "main",
		"body":  body,
	}
	respBody, err := ghPost(token, url, payload)
	if err != nil {
		return "", err
	}
	var result struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	return result.HTMLURL, nil
}

func ghGet(token, url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	return doRequest(req)
}

func ghPost(token, url string, payload interface{}) ([]byte, error) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

func ghPut(token, url string, payload interface{}) ([]byte, error) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

func doRequest(req *http.Request) ([]byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub API %s %s: %d %s", req.Method, req.URL.Path, resp.StatusCode, string(body))
	}
	return body, nil
}

// CreateRepoFromTemplate creates a new repo under javaBin/ from a template repo.
// Returns the clone URL of the new repo.
func CreateRepoFromTemplate(token, templateRepo, name, description string, private bool) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/generate", apiBase, orgName, templateRepo)
	payload := map[string]interface{}{
		"owner":       orgName,
		"name":        name,
		"description": description,
		"private":     private,
	}
	respBody, err := ghPost(token, url, payload)
	if err != nil {
		return "", err
	}
	var result struct {
		CloneURL string `json:"clone_url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	return result.CloneURL, nil
}

// RepoExists checks if a repo exists under javaBin/.
func RepoExists(token, name string) bool {
	url := fmt.Sprintf("%s/repos/%s/%s", apiBase, orgName, name)
	_, err := ghGet(token, url)
	return err == nil
}

func getenv(key string) string {
	return os.Getenv(key)
}
