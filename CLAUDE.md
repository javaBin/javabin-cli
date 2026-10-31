# CLAUDE.md — Javabin CLI

Developer CLI for the Javabin platform, written in Go.

## Project Structure

```
main.go                   Entrypoint — calls cmd.Execute()
cmd/
  root.go                 Cobra root command, registers subcommands
  init.go                 Interactive app scaffolding wizard
  scaffold.go             Per-runtime minimal app + Dockerfile written by init
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
| `javabin register-team` | Interactive wizard — prompts for team name, description, members (google + github), and budget; creates a team registration PR against `javaBin/registry` (`teams/{name}.yaml`, serialised with yaml.v3; first member gets `role: maintainer`) |
| `javabin init` | Interactive wizard — scaffolds a new app repo from `javaBin/app-template`, writes `app.yaml` (with `routing.host`, no `priority`: the platform derives it), `deploy.yml` (with the permissions the platform pipeline needs) and a minimal runtime app from `scaffold.go` (java/Maven image, kotlin/Gradle image, typescript, python, go; no wrapper scripts or lockfiles needed) serving `GET /health` on the chosen port, running as uid 1000 with only `/tmp` written. Commits, adds the repo to the app.yaml GitHub team (`PUT orgs/javaBin/teams/<team>/repos/javaBin/<repo>`), then pushes; if the team add is refused (not a team maintainer/org owner, or no team) it stops before pushing and prints the command for a maintainer plus `git push`. `JAVABIN_SCAFFOLD_DOCKER=1 go test ./cmd -run TestScaffoldImagesServeHealth` builds and runs every scaffold image |
| `javabin status [--team] [--service] [-r]` | Shows month-to-date cost (Cost Explorer, USD with `~NOK` at the platform's 10.5 rate) and ECS service status. Team/service from flags or `./app.yaml`; AWS profile resolved like `secret` (`LoadConfigForTeam`) |
| `javabin secret list` / `javabin secret set <name> [--stdin]` | Run in an app repo: shows which `resources.secrets` from `app.yaml` still hold the platform's `PLACEHOLDER` (never prints values) and overwrites an existing value as an SSM SecureString under `/javabin/apps/{team}/{app}/` (never creates parameters — deploy first). The platform restarts the service automatically when a secret changes. Needs the `javabin-team-<team>` SSO role (`javabin-developer` has no team tag, so it can't reach team parameters). Missing `name:` defaults to the repo name like the pipeline; invalid repo names (`2027.javazone.no`) fail with a suggested `name:` |
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
