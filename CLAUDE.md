# CLAUDE.md — Javabin CLI

Developer CLI for the Javabin platform, written in Go.

## Project Structure

```
main.go                   Entrypoint — calls cmd.Execute()
cmd/
  root.go                 Cobra root command, registers subcommands
  register.go             Interactive app registration wizard
  status.go               Project status (costs, ECS services)
  whoami.go               Show AWS + GitHub identity
internal/
  aws/aws.go              AWS SDK helpers (STS, Cost Explorer, ECS)
  config/config.go        Config directory (~/.javabin/)
  github/github.go        GitHub API client (token, branches, PRs)
```

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/aws/aws-sdk-go-v2` — AWS SDK (STS, Cost Explorer, ECS)
- Go 1.22+

## Authentication

- **GitHub**: Uses `gh auth token` (gh CLI) first, then falls back to `GITHUB_TOKEN` / `GH_TOKEN` environment variables
- **AWS**: Standard credential chain via `aws-sdk-go-v2/config` (env vars, `~/.aws/credentials`, SSO). Region defaults to `eu-central-1`.
- **Cognito**: TODO — will add device flow auth against internal Cognito pool when Identity (Phase 1) is implemented. Token will be cached at `~/.javabin/token`.

## Commands

| Command | What it does |
|---------|-------------|
| `javabin register` | Interactive wizard — prompts for repo, team, auth, budget; creates a registration PR against `javaBin/registry` via GitHub API |
| `javabin status` | Shows month-to-date cost (Cost Explorer) and ECS service status. Infers project from git remote or accepts `--project` flag |
| `javabin whoami` | Shows AWS identity (STS GetCallerIdentity) and GitHub user (gh API) |

## Build and Test

```bash
go build -o javabin .
./javabin --help
```

No tests yet. When adding tests, use standard `go test ./...`.

## Release

Releases are built with GoReleaser on semver tags. Binaries go to GitHub Releases and the Homebrew tap (`javaBin/tap/javabin`).

## Design Decisions

- **No deploy/plan/apply commands** — those run exclusively in CI. The CLI is for registration and status only.
- **No infrastructure management** — use `app.yaml` and let the platform handle it.
- **GitHub API directly** (not `go-github` library) — keeps dependencies minimal for a simple REST client.

## Related

- [javaBin/platform](https://github.com/javaBin/platform) — infrastructure the CLI queries
- [javaBin/registry](https://github.com/javaBin/registry) — where `javabin register` creates PRs
