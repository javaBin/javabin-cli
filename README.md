# javabin CLI

Developer CLI for the Javabin platform.

## Install

```bash
brew install javaBin/tap/javabin        # macOS/Linux via Homebrew
go install github.com/javaBin/javabin-cli@latest  # Go toolchain
```

## Commands

### `javabin register-team`

Interactive wizard to register a new team with the platform. Prompts for team name, description, members (Google handle + GitHub username), and optional budget. Creates a PR against [javaBin/registry](https://github.com/javaBin/registry) with `teams/{name}.yaml`. The first member is written as `role: maintainer`, so they can add repos to the GitHub team.

After your team is created, a maintainer adds existing repos to the GitHub team (`javabin init` does this for new repos):

```bash
gh api -X PUT orgs/javaBin/teams/TEAM/repos/javaBin/REPO -f permission=push
```

```bash
javabin register-team
```

### `javabin init`

Interactive wizard to scaffold a new app repo from the Javabin app template. Creates the repo under `javaBin/`, writes `app.yaml` (routing without a fixed `priority`; the platform derives one), a deploy workflow and a minimal app for the runtime (java/Maven, kotlin/Gradle, typescript, python, go) that builds from a clean checkout and answers `GET /health` on the chosen port, with a `Dockerfile` that runs it as uid 1000 writing only to `/tmp` (as the platform runs it). It then adds the repo to the app's GitHub team and only then pushes to `main`, so the first pipeline run is accepted. If you can't add repos to the team (you need to be a team maintainer or org owner), the scaffold stays committed locally and the CLI prints the command for a maintainer and the `git push` to run afterwards.

```bash
javabin init
```

### `javabin status`

Show month-to-date team cost and ECS service status. Costs are AWS USD amounts shown with their approximate NOK value (team budgets are in NOK). Uses the same AWS profile as `secret`: `--profile`, `AWS_PROFILE`, or your `javabin-team-<team>` profile from `javabin aws setup`.

```bash
javabin status                          # team and service from ./app.yaml
javabin status --team cupcake --service cupcake-web
```

### `javabin secret`

Run in an app repo. `list` shows which `resources.secrets` still hold `PLACEHOLDER`; `set <name>` writes a value. Needs your team role `javabin-team-<team>` (`javabin-developer` cannot reach team secrets). If `app.yaml` has no `name`, the repo name is used, as in the platform pipeline; repo names that aren't valid app names (e.g. `2027.javazone.no`) need an explicit `name:` and the CLI suggests one (`javazone-no-2027`).

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
