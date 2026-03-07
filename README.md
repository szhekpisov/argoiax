# argoiax

Automated Helm chart dependency updates for ArgoCD — named after the mythological helmsman of the Argo.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Tests](https://github.com/szhekpisov/argoiax/actions/workflows/test.yml/badge.svg)](https://github.com/szhekpisov/argoiax/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/szhekpisov/argoiax)](https://github.com/szhekpisov/argoiax/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## Problem

ArgoCD Application manifests pin Helm chart versions in YAML files. When upstream charts release new versions, these pinned versions become stale. Dependabot doesn't support ArgoCD CRDs, leaving teams without automated dependency updates for their GitOps repositories.

**argoiax** fills this gap by scanning your GitOps repo, detecting outdated charts, fetching release notes, and opening Dependabot-style PRs with full context.

## Features

- **HTTP, OCI, and Git registry support** -- works with all Helm repository types
- **ApplicationSet support** -- handles both `Application` and `ApplicationSet` resources
- **Multi-source applications** -- correctly updates charts in multi-source ArgoCD manifests
- **Breaking change detection** -- flags major version bumps with warnings
- **Release notes** -- fetches notes from GitHub Releases, ArtifactHub, and changelogs
- **Dependabot-style PRs** -- creates well-formatted pull requests with release notes and breaking change badges
- **Version constraints** -- respects semver constraints per chart
- **Configurable** -- control PR strategy, labels, branch naming, and more

## Running locally

### Prerequisites

- Go 1.26+
- A GitHub token (only needed for the `update` command)

### Install from source

```bash
go install github.com/szhekpisov/argoiax@latest
```

Or clone and build:

```bash
git clone https://github.com/szhekpisov/argoiax.git
cd argoiax
make build        # binary at ./bin/argoiax
make install      # installs to $GOPATH/bin
```

You can also download a pre-built binary from [Releases](https://github.com/szhekpisov/argoiax/releases).

### Scan for outdated charts

```bash
# Scan the current directory (or scanDirs from argoiax.yaml)
argoiax scan

# Scan a specific directory
argoiax scan --dir apps/

# Output as JSON or Markdown
argoiax scan --dir apps/ -o json
argoiax scan --dir apps/ -o markdown

# Show all charts including up-to-date ones
argoiax scan --dir apps/ --show-uptodate

# Exit with non-zero code if outdated charts are found (useful in CI)
argoiax scan --dir apps/ --fail-on-drift

# Only check a specific chart
argoiax scan --dir apps/ --chart cert-manager
```

### Create update PRs

```bash
# Set your GitHub token
export GITHUB_TOKEN=ghp_...

# Create PRs for all outdated charts
argoiax update --dir apps/ --repo owner/repo

# Dry run — show what would be updated without creating PRs
argoiax update --dir apps/ --repo owner/repo --dry-run

# Include major version updates (skipped by default)
argoiax update --dir apps/ --repo owner/repo --allow-major

# Limit the number of PRs created
argoiax update --dir apps/ --repo owner/repo --max-prs 5

# Use a custom config file
argoiax update --config path/to/argoiax.yaml --repo owner/repo

# Enable debug logging
argoiax update --dir apps/ --repo owner/repo --log-level debug
```

## Running via GitHub Action

Add a workflow file to your GitOps repository (e.g. `.github/workflows/argoiax.yml`):

### Basic setup

```yaml
name: Helm Chart Updates
on:
  schedule:
    - cron: '0 8 * * 1-5'  # Weekdays at 8am UTC
  workflow_dispatch:        # Allow manual trigger

jobs:
  update:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
      - uses: szhekpisov/argoiax@main
        with:
          command: update
          github-token: ${{ secrets.GITHUB_TOKEN }}
```

### Scan only (e.g. for CI checks)

```yaml
name: Chart Drift Check
on:
  pull_request:
    branches: [main]

jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: szhekpisov/argoiax@main
        with:
          command: scan
          dir: apps/
```

### Advanced setup with all options

```yaml
name: Helm Chart Updates
on:
  schedule:
    - cron: '0 8 * * 1-5'
  workflow_dispatch:

jobs:
  update:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@v4

      - uses: szhekpisov/argoiax@main
        id: argoiax
        with:
          command: update
          config: argoiax.yaml
          dir: apps/
          chart: ''              # leave empty to update all charts
          allow-major: 'false'
          max-prs: '10'
          log-level: info
          github-token: ${{ secrets.GITHUB_TOKEN }}

      - name: Summary
        run: |
          echo "Updates found: ${{ steps.argoiax.outputs.updates-found }}"
          echo "PRs created: ${{ steps.argoiax.outputs.prs-created }}"
```

### Action inputs

| Input | Description | Default |
|-------|-------------|---------|
| `command` | Command to run (`scan` or `update`) | `update` |
| `config` | Path to config file | `argoiax.yaml` |
| `dir` | Directory to scan | `.` |
| `chart` | Only check/update a specific chart | |
| `allow-major` | Include major version updates | `false` |
| `max-prs` | Maximum PRs to create | `10` |
| `github-token` | GitHub token | `${{ github.token }}` |
| `log-level` | Log level | `info` |
| `version` | Tool version to use | `latest` |

### Action outputs

| Output | Description |
|--------|-------------|
| `updates-found` | Number of outdated charts detected |
| `prs-created` | Number of PRs created |

## CLI reference

### `argoiax scan`

Scan for outdated Helm chart versions in ArgoCD manifests.

```
Flags:
  -o, --output string     Output format: table, json, markdown (default "table")
      --chart string      Only check a specific chart name
      --show-uptodate     Include up-to-date charts in output
      --fail-on-drift     Exit with non-zero code when outdated charts are found
```

### `argoiax update`

Create PRs for outdated Helm chart versions.

```
Flags:
      --chart string          Only update a specific chart name
      --allow-major           Include major version updates
      --max-prs int           Maximum number of PRs to create (0 = use config)
      --github-token string   GitHub token (or set GITHUB_TOKEN env var)
      --repo string           GitHub repository (owner/repo)
```

### `argoiax version`

Print the version, commit, and build date.

### Global flags

```
      --config string      Config file (defaults to argoiax.yaml if not specified)
      --dir string         Directory to scan (defaults to scanDirs from config, which defaults to ["."])
      --dry-run            Report changes without modifying files
      --log-level string   Log level: debug, info, warn, error (default "info")
```

## Configuration

Create an `argoiax.yaml` file in your repository root (see [argoiax.yaml.example](argoiax.yaml.example)):

```yaml
version: 1
scanDirs: [apps/, clusters/production/]
ignore: ["**/test/**"]

charts:
  cert-manager:
    versionConstraint: ">=1.0.0, <2.0.0"
  "https://charts.bitnami.com/bitnami#postgresql":
    githubRepo: "bitnami/charts"
    tagPattern: "postgresql-{{.Version}}"

settings:
  prStrategy: "per-chart"          # per-chart | per-file | batch
  labels: [argoiax, dependencies]
  baseBranch: main
  branchTemplate: "argoiax/{{.ChartName}}-{{.NewVersion}}"
  titleTemplate: "chore(deps): update {{.ChartName}} to {{.NewVersion}}"
  groupBranchTemplate: "argoiax/update-{{.FileBaseName}}"
  groupTitleTemplate: "chore(deps): update {{.Count}} chart(s) in {{.FileBaseName}}"
  maxOpenPRs: 10

auth:
  helmRepos:
    - url: "https://private.example.com"
      username: "${HELM_USER}"
      password: "${HELM_PASS}"
  # Private OCI registries are authenticated via Docker credentials
  # (~/.docker/config.json, credential helpers, or `docker login`).
  # Explicit OCI auth configuration in this file is not yet supported.

releaseNotes:
  enabled: true
  maxLength: 10000
  includeIntermediate: true
  sources: [github-releases, artifacthub, changelog]
```

### Key settings

| Setting | Description | Default |
|---------|-------------|---------|
| `prStrategy` | PR grouping: `per-chart`, `per-file`, or `batch` | `per-chart` |
| `baseBranch` | Target branch for PRs | `main` |
| `branchTemplate` | Branch name template for per-chart PRs | `argoiax/{{.ChartName}}-{{.NewVersion}}` |
| `titleTemplate` | PR title template for per-chart PRs | `chore(deps): update {{.ChartName}} to {{.NewVersion}}` |
| `groupBranchTemplate` | Branch name template for per-file/batch PRs | `argoiax/update-{{.FileBaseName}}` |
| `groupTitleTemplate` | PR title template for per-file/batch PRs | `chore(deps): update {{.Count}} chart(s) in {{.FileBaseName}}` |
| `maxOpenPRs` | Maximum concurrent open PRs | `10` |
| `releaseNotes.enabled` | Fetch and include release notes in PRs | `true` |
| `releaseNotes.sources` | Release note sources in priority order | `[github-releases, artifacthub, changelog]` |

## Supported ArgoCD patterns

| Pattern | Example | Supported |
|---------|---------|-----------|
| Single-source HTTP | `source.repoURL: https://charts.jetstack.io` | Yes |
| Single-source OCI | `source.repoURL: oci://ghcr.io/org/charts` | Yes |
| Single-source Git | `source.repoURL: https://github.com/org/repo.git` | Yes |
| Multi-source | `sources[].repoURL` with mixed types | Yes |
| ApplicationSet | `spec.template.spec.source` | Yes |

## License

[MIT](LICENSE)
