package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

func writeAppYaml(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSecretPathMatchesServiceSecretModule(t *testing.T) {
	app, err := loadAppYaml(writeAppYaml(t, "name: cupcake-backend\nteam: cupcake\nresources:\n  secrets:\n    - name: sp_user\n      env: SP_USER\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := secretPath(app, "sp_user"); got != "/javabin/apps/cupcake/cupcake-backend/sp_user" {
		t.Errorf("secretPath = %s", got)
	}
	if !app.declares("sp_user") || app.declares("sp_password") {
		t.Error("declares() should only accept secrets listed in app.yaml")
	}
}

func TestLoadAppYamlRequiresNameAndTeam(t *testing.T) {
	if _, err := loadAppYaml(writeAppYaml(t, "name: x\n")); err == nil {
		t.Error("expected an error without team")
	}
}

func TestReadSecretValueFromStdinTrimsNewline(t *testing.T) {
	v, err := readSecretValue(strings.NewReader("s3cret\n"), true, "sp_user")
	if err != nil || v != "s3cret" {
		t.Errorf("got %q, %v", v, err)
	}
}

func TestReadSecretValueRejectsEmptyAndPlaceholder(t *testing.T) {
	for _, in := range []string{"\n", "PLACEHOLDER\n"} {
		if _, err := readSecretValue(strings.NewReader(in), true, "sp_user"); err == nil {
			t.Errorf("expected rejection for %q", in)
		}
	}
}

type fakeSSM struct {
	exists bool
	puts   []*ssm.PutParameterInput
}

func (f *fakeSSM) GetParameter(_ context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	if !f.exists {
		return nil, &ssmtypes.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Name: in.Name, Value: aws.String(placeholder)}}, nil
}

func (f *fakeSSM) PutParameter(_ context.Context, in *ssm.PutParameterInput, _ ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	f.puts = append(f.puts, in)
	return &ssm.PutParameterOutput{}, nil
}

func TestSetSecretRefusesToCreateMissingParameter(t *testing.T) {
	f := &fakeSSM{exists: false}
	err := setSecret(context.Background(), f, "/javabin/apps/t/a/x", "v")
	if err == nil || !strings.Contains(err.Error(), "does not exist yet") {
		t.Fatalf("expected not-exist error, got %v", err)
	}
	if len(f.puts) != 0 {
		t.Error("PutParameter must not be called for a missing parameter")
	}
}

func TestSetSecretOverwritesExistingParameter(t *testing.T) {
	f := &fakeSSM{exists: true}
	if err := setSecret(context.Background(), f, "/javabin/apps/t/a/x", "v"); err != nil {
		t.Fatal(err)
	}
	if len(f.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(f.puts))
	}
	p := f.puts[0]
	if aws.ToString(p.Value) != "v" || !aws.ToBool(p.Overwrite) || p.Type != ssmtypes.ParameterTypeSecureString {
		t.Errorf("unexpected put input: %+v", p)
	}
}
