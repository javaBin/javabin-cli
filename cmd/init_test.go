package cmd

import (
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
	for _, want := range []string{"id-token: write", "contents: write", "pull-requests: write"} {
		if !strings.Contains(deployYaml, want) {
			t.Errorf("deploy.yml missing %q", want)
		}
	}
	if strings.Contains(deployYaml, "contents: read") {
		t.Error("deploy.yml must not restrict contents to read")
	}
}
