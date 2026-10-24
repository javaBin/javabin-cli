package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderAppYamlIncludesRouting(t *testing.T) {
	got := renderAppYaml("moresleep", "core", "8080", "moresleep.javazone.no", "120")
	for _, want := range []string{
		"name: moresleep\n",
		"team: core\n",
		"  port: 8080\n",
		"routing:\n  host: moresleep.javazone.no\n  priority: 120\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("app.yaml missing %q:\n%s", want, got)
		}
	}
}

func TestDeployYamlGrantsPipelinePermissions(t *testing.T) {
	// expand commits generated Terraform, so contents must be writable
	for _, want := range []string{"workflow_dispatch:", "id-token: write", "contents: write", "pull-requests: write"} {
		if !strings.Contains(deployYaml, want) {
			t.Errorf("deploy.yml missing %q", want)
		}
	}
	if strings.Contains(deployYaml, "secrets: inherit") {
		t.Error("deploy.yml must not use secrets: inherit")
	}
	if strings.Contains(deployYaml, "contents: read") {
		t.Error("deploy.yml must not restrict contents to read")
	}
}

// TestRenderAppYamlGolden pins the full output; it is the file validated
// against platform/scripts/generate-modules.py.
func TestRenderAppYamlGolden(t *testing.T) {
	got := renderAppYaml("initcheck", "platform", "8080", "initcheck.javazone.no", "4242")
	if dir := os.Getenv("INIT_APP_YAML_OUT"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := `name: initcheck
team: platform

compute:
  port: 8080
  health_check: /health

routing:
  host: initcheck.javazone.no
  priority: 4242
`
	if got != want {
		t.Errorf("app.yaml =\n%s\nwant\n%s", got, want)
	}
}
