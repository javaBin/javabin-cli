# CLAUDE.md — Javabin CLI

Developer CLI for the Javabin platform, written in Go.

## Project Structure

```
main.go                   Entrypoint — calls cmd.Execute()
cmd/
  root.go                 Cobra root command, registers subcommands
  init.go                 Interactive app scaffolding wizard
  register.go             Interactive team registration wizard
  status.go               Project status (costs, ECS services)
  whoami.go               Show AWS + GitHub identity
  secret.go               List/set app.yaml secrets in SSM
  aws.go                  aws setup / aws doctor (SSO config for javaBin)
internal/
  aws/aws.go              AWS SDK helpers (STS, Cost Explorer, ECS)
  aws/profile.go          --profile and team-profile auto-discovery
  awsconfig/              Edits ~/.aws/config preserving unrelated content; SSO token cache + ListAccountRoles
  config/config.go        Config directory (~/.javabin/)
  github/github.go        GitHub API client (token, branches, PRs)
```

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/aws/aws-sdk-go-v2` — AWS SDK (STS, Cost Explorer, ECS)
- Go 1.22+

## Authentication

- **GitHub**: Uses `gh auth token` (gh CLI) first, then falls back to `GITHUB_TOKEN` / `GH_TOKEN` environment variables
- **AWS**: Standard credential chain via `aws-sdk-go-v2/config` (env vars, `~/.aws/credentials`, SSO). Region defaults to `eu-central-1`. Team commands resolve `--profile`, then `AWS_PROFILE`, then the single profile with `sso_role_name = javabin-team-<team>` (written by `javabin aws setup`); without one the SDK would hang on EC2 IMDS, so `credentialsHelp` points at `javabin aws setup`.
- **Cognito**: TODO — will add device flow auth against internal Cognito pool when Identity (Phase 1) is implemented. Token will be cached at `~/.javabin/token`.

## Commands

| Command | What it does |
|---------|-------------|
| `javabin register-team` | Interactive wizard — prompts for team name, description, members (google + github), and budget; creates a team registration PR against `javaBin/registry` (`teams/{name}.yaml`) |
| `javabin init` | Interactive wizard — scaffolds a new app repo from `javaBin/app-template`, writes `app.yaml` (with `routing:`), a runtime `Dockerfile` (Maven for java, Gradle for kotlin) and `deploy.yml` (with the permissions the platform pipeline needs), then prints next steps: the command to add the repo to the app.yaml team, `javabin aws setup`, and `javabin secret set` after the first deploy |
| `javabin status` | Shows month-to-date cost (Cost Explorer) and ECS service status. Infers project from git remote or accepts `--project` flag |
| `javabin secret list` / `javabin secret set <name> [--stdin]` | Run in an app repo: shows which `resources.secrets` from `app.yaml` still hold the platform's `PLACEHOLDER` (never prints values) and overwrites an existing value as an SSM SecureString under `/javabin/apps/{team}/{app}/` (never creates parameters — deploy first). The platform restarts the service automatically when a secret changes. Needs the `javabin-developer` SSO role |
| `javabin aws setup [--dry-run] [--no-login] [--force]` | Adds `[sso-session javabin]` (Identity Center start URL, `eu-central-1`) to `AWS_CONFIG_FILE`/`~/.aws/config`, runs `aws sso login --sso-session javabin`, lists the user's roles in account 553637109631 (SSO ListAccountRoles with the cached token) and writes one `[profile <role>]` per role (`javabin-team-<team>`, `javabin-developer`, ...; non-`javabin-` roles get a `javabin-` prefix). Never edits/deletes existing profiles (conflicts are reported), skips roles another `sso_session = javabin` profile already uses (profiles on other sessions are kept but warned about, as is a role with several profiles, which turns off team-profile auto-discovery), backs up to `<target>.bak-<ts>` (symlinks are followed and kept), writes atomically with 0600. A session pointing at another start URL stops unless `--force` |
| `javabin aws doctor [--app-yaml]` | Checks the session block, SSO token expiry, and which profile `secret` would pick for `app.yaml`'s team |
| `javabin whoami` | Shows AWS identity (STS GetCallerIdentity) and GitHub user (gh API) |

## Build and Test

```bash
go build -o javabin .
./javabin --help
```

```bash
go test ./...
```

## Release

Releases are built with GoReleaser on semver tags. Binaries go to GitHub Releases and the Homebrew tap (`javaBin/tap/javabin`).

## Design Decisions

- **No deploy/plan/apply commands** — those run exclusively in CI. The CLI covers registration, scaffolding, status and setting app secret values.
- **No infrastructure management** — use `app.yaml` and let the platform handle it.
- **GitHub API directly** (not `go-github` library) — keeps dependencies minimal for a simple REST client.

## Related

- [javaBin/platform](https://github.com/javaBin/platform) — infrastructure the CLI queries
- [javaBin/registry](https://github.com/javaBin/registry) — where `javabin register-team` creates PRs
