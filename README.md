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

Interactive wizard to scaffold a new app repo from the Javabin app template. Creates the repo under `javaBin/`, writes `app.yaml`, `Dockerfile`, and a deploy workflow, then prints next steps: adding the repo to your GitHub team, `javabin aws setup` for AWS access, and `javabin secret set` for secrets after the first deploy.

```bash
javabin init
```

### `javabin status`

Show project status: costs, ECS services, deployments.

```bash
javabin status              # infers project from git remote
javabin status --project moresleep
```

### `javabin aws setup`

One-time AWS setup — no hand-editing of `~/.aws/config`. Requires the [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) (`brew install awscli`).

```bash
javabin aws setup             # add SSO session, sign in, write a profile per role you have
javabin aws setup --dry-run   # show what would change
javabin aws setup --no-login  # reuse a valid SSO session
javabin aws doctor            # check config, token and the profile `secret` would use
```

It adds an `[sso-session javabin]` block for javaBin's IAM Identity Center, runs `aws sso login --sso-session javabin`, then writes one profile per role you are assigned in account `553637109631`. Profiles are named after the role: `javabin-team-<team>`, `javabin-developer`, `javabin-admin`, `javabin-readonly`. Existing profiles are never changed; the config is backed up to `config.bak-<timestamp>` first. `javabin secret ...` picks your team's profile automatically; elsewhere use `--profile javabin-team-<team>` or `AWS_PROFILE`. Sessions expire after ~8h: `aws sso login --sso-session javabin`.

### `javabin whoami`

Show current identity (AWS + GitHub).

```bash
javabin whoami
```

## Authentication

- **GitHub:** Uses `gh auth token` if available, or `GITHUB_TOKEN`/`GH_TOKEN` environment variables
- **AWS:** Standard credential chain (environment variables, `~/.aws/credentials`, SSO). Run `javabin aws setup` once to get SSO profiles.

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
