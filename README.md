# javabin CLI

Developer CLI for the Javabin platform.

## Install

```bash
brew install javaBin/tap/javabin        # macOS/Linux via Homebrew
go install github.com/javaBin/javabin-cli@latest  # Go toolchain
```

## Commands

### `javabin register-team`

Interactive wizard to register a new team with the platform. Prompts for team name, description, members (Google handle + GitHub username), and optional budget. Creates a PR against [javaBin/registry](https://github.com/javaBin/registry) with `teams/{name}.yaml`.

After your team is created, add repos to your GitHub team:

```bash
gh api orgs/javaBin/teams/TEAM/repos -f owner=javaBin -f repo=REPO -f permission=push
```

```bash
javabin register-team
```

### `javabin init`

Interactive wizard to scaffold a new app repo from the Javabin app template. Creates the repo under `javaBin/`, writes `app.yaml`, `Dockerfile`, and a deploy workflow, then prints next steps for adding the repo to your GitHub team.

```bash
javabin init
```

### `javabin status`

Show project status: costs, ECS services, deployments.

```bash
javabin status              # infers project from git remote
javabin status --project moresleep
```

### `javabin whoami`

Show current identity (AWS + GitHub).

```bash
javabin whoami
```

## Authentication

- **GitHub:** Uses `gh auth token` if available, or `GITHUB_TOKEN`/`GH_TOKEN` environment variables
- **AWS:** Standard credential chain (environment variables, `~/.aws/credentials`, SSO)

## What This CLI Does NOT Do

- No deploy, plan, apply, or generate commands — those run exclusively in CI
- No infrastructure management — use `app.yaml` and let the platform handle it

## Development

```bash
go build -o javabin .
./javabin --help
```

## Release

Releases are built with [GoReleaser](https://goreleaser.com/) on semver tags. Binaries are published to GitHub Releases and the Homebrew tap.
