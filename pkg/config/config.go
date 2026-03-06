package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// PR strategy constants.
const (
	StrategyPerChart = "per-chart"
	StrategyPerFile  = "per-file"
	StrategyBatch    = "batch"
)

// Release notes source constants.
const (
	SourceGitHubReleases = "github-releases"
	SourceArtifactHub    = "artifacthub"
	SourceChangelog      = "changelog"
)

// Config is the top-level configuration for argoiax.
type Config struct {
	Version  int                `yaml:"version"`
	ScanDirs []string           `yaml:"scanDirs"`
	Ignore   []string           `yaml:"ignore"`
	Charts   map[string]Chart   `yaml:"charts"`
	Settings Settings           `yaml:"settings"`
	Auth     Auth               `yaml:"auth"`
	Release  ReleaseNotesConfig `yaml:"releaseNotes"`
}

// Chart holds per-chart configuration overrides.
type Chart struct {
	VersionConstraint string `yaml:"versionConstraint"`
	GithubRepo        string `yaml:"githubRepo"`
	TagPattern        string `yaml:"tagPattern"`
}

// Settings controls PR creation behavior and templates.
type Settings struct {
	PRStrategy          string   `yaml:"prStrategy"`
	Labels              []string `yaml:"labels"`
	BaseBranch          string   `yaml:"baseBranch"`
	BranchTemplate      string   `yaml:"branchTemplate"`
	TitleTemplate       string   `yaml:"titleTemplate"`
	GroupBranchTemplate string   `yaml:"groupBranchTemplate"`
	GroupTitleTemplate  string   `yaml:"groupTitleTemplate"`
	MaxOpenPRs          int      `yaml:"maxOpenPRs"`
	AutoMergePatch      bool     `yaml:"autoMergePatch"`
}

// Auth holds authentication configuration for registries.
type Auth struct {
	HelmRepos     []HelmRepoAuth `yaml:"helmRepos"`
	OCIRegistries []OCIAuth      `yaml:"ociRegistries"`
}

// HelmRepoAuth holds credentials for a Helm HTTP repository.
type HelmRepoAuth struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// OCIAuth holds authentication configuration for an OCI registry.
type OCIAuth struct {
	Registry string `yaml:"registry"`
	Provider string `yaml:"provider"`
}

// ReleaseNotesConfig controls release notes fetching behavior.
type ReleaseNotesConfig struct {
	Enabled             bool     `yaml:"enabled"`
	MaxLength           int      `yaml:"maxLength"`
	IncludeIntermediate bool     `yaml:"includeIntermediate"`
	Sources             []string `yaml:"sources"`
}

// DefaultConfig returns the default argoiax configuration.
func DefaultConfig() *Config {
	return &Config{
		Version:  1,
		ScanDirs: []string{"."},
		Settings: Settings{
			PRStrategy:          StrategyPerChart,
			Labels:              []string{"argoiax", "dependencies"},
			BaseBranch:          "main",
			BranchTemplate:      "argoiax/{{.ChartName}}-{{.NewVersion}}",
			TitleTemplate:       "chore(deps): update {{.ChartName}} to {{.NewVersion}}",
			GroupBranchTemplate: "argoiax/update-{{.FileBaseName}}",
			GroupTitleTemplate:  "chore(deps): update {{.Count}} chart(s) in {{.FileBaseName}}",
			MaxOpenPRs:          10,
			AutoMergePatch:      false,
		},
		Release: ReleaseNotesConfig{
			Enabled:             true,
			MaxLength:           10000,
			IncludeIntermediate: true,
			Sources:             []string{SourceGitHubReleases, SourceArtifactHub, SourceChangelog},
		},
	}
}

// Load reads and parses the config file, falling back to defaults when the path is empty.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		path = "argoiax.yaml"
	}

	data, err := os.ReadFile(path) //nolint:gosec // path from user-specified --config flag
	if err != nil {
		if os.IsNotExist(err) && path == "argoiax.yaml" {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Expand environment variables in config
	expanded := os.ExpandEnv(string(data))

	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

var validStrategies = map[string]bool{StrategyPerChart: true, StrategyPerFile: true, StrategyBatch: true}
var validSources = map[string]bool{SourceGitHubReleases: true, SourceArtifactHub: true, SourceChangelog: true}

// Validate checks that all config values are valid.
func (c *Config) Validate() error {
	if c.Version != 0 && c.Version != 1 {
		return fmt.Errorf("unsupported config version: %d", c.Version)
	}

	if c.Settings.PRStrategy != "" && !validStrategies[c.Settings.PRStrategy] {
		return fmt.Errorf("invalid prStrategy %q (must be %s, %s, or %s)", c.Settings.PRStrategy, StrategyPerChart, StrategyPerFile, StrategyBatch)
	}

	for _, src := range c.Release.Sources {
		if !validSources[src] {
			return fmt.Errorf("invalid release notes source %q", src)
		}
	}

	return nil
}

// LookupChart finds chart config by name or by "repoURL#chartName" key.
func (c *Config) LookupChart(name, repoURL string) *Chart {
	if ch, ok := c.Charts[name]; ok {
		return &ch
	}
	key := repoURL + "#" + name
	if ch, ok := c.Charts[key]; ok {
		return &ch
	}
	return nil
}

// FindRepoAuth returns auth config for a given repo URL.
func (c *Config) FindRepoAuth(repoURL string) *HelmRepoAuth {
	for i := range c.Auth.HelmRepos {
		if repoURL == c.Auth.HelmRepos[i].URL || strings.HasPrefix(repoURL, c.Auth.HelmRepos[i].URL+"/") {
			return &c.Auth.HelmRepos[i]
		}
	}
	return nil
}
