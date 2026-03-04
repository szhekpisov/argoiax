# ancaeus

Automated Helm chart dependency updates for ArgoCD — named after the mythological helmsman of the Argo.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![CI](https://github.com/vertrost/ancaeus/actions/workflows/ci.yml/badge.svg)](https://github.com/vertrost/ancaeus/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/vertrost/ancaeus)](https://github.com/vertrost/ancaeus/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## Problem

ArgoCD Application manifests pin Helm chart versions in YAML files. When upstream charts release new versions, these pinned versions become stale. Dependabot doesn't support ArgoCD CRDs, leaving teams without automated dependency updates for their GitOps repositories.

**ancaeus** fills this gap by scanning your GitOps repo, detecting outdated charts, fetching release notes, and opening Dependabot-style PRs with full context.

## Features

- **HTTP, OCI, and Git registry support** -- works with all Helm repository types
- **ApplicationSet support** -- handles both `Application` and `ApplicationSet` resources
- **Multi-source applications** -- correctly updates charts in multi-source ArgoCD manifests
- **Breaking change detection** -- flags major version bumps with warnings
- **Release notes** -- fetches notes from GitHub Releases, ArtifactHub, and changelogs
- **Dependabot-style PRs** -- creates well-formatted pull requests with release notes and breaking change badges
- **Version constraints** -- respects semver constraints per chart
- **Configurable** -- control PR strategy, labels, branch naming, and more

## Quick start

### Install

```bash
go install github.com/vertrost/ancaeus@latest
```

Or download a binary from [Releases](https://github.com/vertrost/ancaeus/releases).

### Scan for outdated charts

```bash
ancaeus scan --dir apps/
```

### Create update PRs

```bash
export GITHUB_TOKEN=ghp_...
ancaeus update --dir apps/ --repo owner/repo
```

## CLI reference

### `scan`

Scan for outdated Helm chart versions in ArgoCD manifests.

```
ancaeus scan [flags]

Flags:
  -o, --output string     Output format: table, json, markdown (default "table")
      --chart string      Only check a specific chart name
      --show-uptodate     Include up-to-date charts in output
```

### `update`

Create PRs for outdated Helm chart versions.

```
ancaeus update [flags]

Flags:
      --chart string          Only update a specific chart name
      --allow-major           Include major version updates
      --max-prs int           Maximum number of PRs to create (0 = use config)
      --github-token string   GitHub token (or set GITHUB_TOKEN env var)
      --repo string           GitHub repository (owner/repo)
```

### `version`

Print the version.

```
ancaeus version
```

### Global flags

```
      --config string      Config file (default "ancaeus.yaml")
      --dir string         Directory to scan (default ".")
      --dry-run            Report changes without modifying files
      --log-level string   Log level: debug, info, warn, error (default "info")
```

## Configuration

Create an `ancaeus.yaml` file in your repository root:

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
  labels: [ancaeus, dependencies]
  baseBranch: main
  branchTemplate: "ancaeus/{{.ChartName}}-{{.NewVersion}}"
  titleTemplate: "chore(deps): update {{.ChartName}} to {{.NewVersion}}"
  maxOpenPRs: 10
  autoMergePatch: true

auth:
  helmRepos:
    - url: "https://private.example.com"
      username: "${HELM_USER}"
      password: "${HELM_PASS}"
  ociRegistries:
    - registry: "123456789.dkr.ecr.us-east-1.amazonaws.com"
      provider: ecr

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
| `maxOpenPRs` | Maximum concurrent open PRs | `10` |
| `autoMergePatch` | Auto-merge label for patch updates | `false` |
| `releaseNotes.enabled` | Fetch and include release notes in PRs | `true` |
| `releaseNotes.sources` | Release note sources in priority order | `[github-releases, artifacthub, changelog]` |

## GitHub Action

```yaml
name: Helm Chart Updates
on:
  schedule:
    - cron: '0 8 * * 1-5'  # Weekdays at 8am
  workflow_dispatch:

jobs:
  update:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
      - uses: vertrost/ancaeus@v1
        with:
          command: update
          dir: apps/
          github-token: ${{ secrets.GITHUB_TOKEN }}
```

### Action inputs

| Input | Description | Default |
|-------|-------------|---------|
| `command` | Command to run (`scan` or `update`) | `update` |
| `config` | Path to config file | `ancaeus.yaml` |
| `dir` | Directory to scan | `.` |
| `chart` | Only check/update a specific chart | |
| `allow-major` | Include major version updates | `false` |
| `max-prs` | Maximum PRs to create | `10` |
| `github-token` | GitHub token | `${{ github.token }}` |
| `log-level` | Log level | `info` |
| `version` | Tool version to use | `latest` |

## Docker

```bash
docker run --rm ghcr.io/vertrost/ancaeus scan --dir /data
```

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
