package cmd

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderTeamYamlFirstMemberIsMaintainerAndQuotesDescription(t *testing.T) {
	desc := "Video: recording # and streaming"
	got, err := renderTeamYaml("video", desc, []member{
		{Google: "ada.lovelace", GitHub: "ada"},
		{Google: "alan.turing", GitHub: "alan"},
	}, "750")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Members     []struct {
			Google string `yaml:"google"`
			GitHub string `yaml:"github"`
			Role   string `yaml:"role"`
		} `yaml:"members"`
		BudgetNOK int `yaml:"budget_nok"`
	}
	if err := yaml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, got)
	}
	if parsed.Name != "video" || parsed.Description != desc || parsed.BudgetNOK != 750 {
		t.Errorf("round trip lost data:\n%s", got)
	}
	if len(parsed.Members) != 2 || parsed.Members[0].Role != "maintainer" || parsed.Members[1].Role != "" {
		t.Errorf("first member must be the only maintainer:\n%s", got)
	}
	if parsed.Members[1].GitHub != "alan" || parsed.Members[0].Google != "ada.lovelace" {
		t.Errorf("members wrong:\n%s", got)
	}
}

func TestRenderTeamYamlDefaultBudgetOmitted(t *testing.T) {
	got, err := renderTeamYaml("video", "Video", []member{{Google: "a.b", GitHub: "ab"}}, "500")
	if err != nil {
		t.Fatal(err)
	}
	want := "name: video\ndescription: Video\nmembers:\n  - google: a.b\n    github: ab\n    role: maintainer\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := renderTeamYaml("video", "Video", []member{{Google: "a.b", GitHub: "ab"}}, "lots"); err == nil {
		t.Error("expected an error for a non-numeric budget")
	}
}
