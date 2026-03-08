package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/szhekpisov/argoiax/pkg/config"
	"github.com/szhekpisov/argoiax/pkg/manifest"
	"github.com/szhekpisov/argoiax/pkg/pr"
)

type mockCreator struct {
	existing map[string]int // branch → PR number (0 means no PR)
	prs      []*pr.UpdateInfo
	groupPRs []pr.UpdateGroup
}

func (m *mockCreator) ExistingPR(_ context.Context, branch string) (int, error) {
	return m.existing[branch], nil
}

func (m *mockCreator) UpdatePRBody(_ context.Context, _ int, _ string) error {
	return nil
}

func (m *mockCreator) CreatePR(_ context.Context, info *pr.UpdateInfo, _ []byte, _ string) (*pr.Result, error) {
	m.prs = append(m.prs, info)
	n := len(m.prs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

func (m *mockCreator) CreateGroupPR(_ context.Context, group pr.UpdateGroup, _ string) (*pr.Result, error) {
	m.groupPRs = append(m.groupPRs, group)
	n := len(m.groupPRs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

// errMockCreator is a mock Creator that returns errors from its methods.
type errMockCreator struct {
	existingErr    error
	existingVal    int
	createPRErr    error
	groupPRErr     error
	updateBodyErr  error
	updateBodyCall int // counts UpdatePRBody invocations
	prs            []*pr.UpdateInfo
	groupPRs       []pr.UpdateGroup
}

func (m *errMockCreator) ExistingPR(_ context.Context, _ string) (int, error) {
	if m.existingErr != nil {
		return 0, m.existingErr
	}
	return m.existingVal, nil
}

func (m *errMockCreator) UpdatePRBody(_ context.Context, _ int, _ string) error {
	m.updateBodyCall++
	return m.updateBodyErr
}

func (m *errMockCreator) CreatePR(_ context.Context, info *pr.UpdateInfo, _ []byte, _ string) (*pr.Result, error) {
	if m.createPRErr != nil {
		return nil, m.createPRErr
	}
	m.prs = append(m.prs, info)
	n := len(m.prs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

func (m *errMockCreator) CreateGroupPR(_ context.Context, group pr.UpdateGroup, _ string) (*pr.Result, error) {
	if m.groupPRErr != nil {
		return nil, m.groupPRErr
	}
	m.groupPRs = append(m.groupPRs, group)
	n := len(m.groupPRs)
	return &pr.Result{PRURL: fmt.Sprintf("https://github.com/x/y/pull/%d", n), PRNumber: n}, nil
}

func newTestHelmServer(t *testing.T) *httptest.Server {
	t.Helper()
	idx := `entries:
  mychart:
    - version: "1.0.0"
    - version: "1.1.0"
    - version: "1.2.0"
  otherchart:
    - version: "2.0.0"
    - version: "3.0.0"
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.yaml" {
			_, _ = fmt.Fprint(w, idx)
		} else {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeTestManifest(t *testing.T, dir, name, chart, version string) string {
	t.Helper()
	content := fmt.Sprintf("apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: %s\nspec:\n  source:\n    repoURL: https://charts.example.com\n    chart: %s\n    targetRevision: %s\n", name, chart, version)
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeUpdate(filePath, chartName, oldVer, newVer string) resolvedUpdate {
	return resolvedUpdate{
		ref: manifest.ChartReference{
			ChartName: chartName, RepoURL: "https://charts.example.com",
			TargetRevision: oldVer, Type: manifest.SourceTypeHTTP,
			FilePath: filePath, YAMLPath: "spec.source.targetRevision", SourceIndex: -1,
		},
		info: pr.UpdateInfo{
			ChartName: chartName, OldVersion: oldVer, NewVersion: newVer,
			FilePath: filePath, RepoURL: "https://charts.example.com",
		},
	}
}

func testSettings() config.Settings {
	return config.Settings{
		BaseBranch:          "main",
		BranchTemplate:      "argoiax/{{.ChartName}}-{{.NewVersion}}",
		TitleTemplate:       "update {{.ChartName}} to {{.NewVersion}}",
		GroupBranchTemplate: "argoiax/update-{{.FileBaseName}}",
		GroupTitleTemplate:  "update {{.Count}} charts",
		MaxOpenPRs:          10,
	}
}

// newMockGitHubAPI creates a test server that mocks the GitHub API endpoints
// needed for default branch detection and PR existence checks.
// ExistingPR always returns true (existing PR found), so the test avoids
// needing to mock the full PR creation flow (branch, commit, PR endpoints).
func newMockGitHubAPI(t *testing.T, defaultBranch string) *github.Client {
	t.Helper()
	return newMockGitHubAPIWithBranches(t, defaultBranch, nil)
}

// newMockGitHubAPIWithBranches creates a mock GitHub API that returns the given
// default branch and only accepts refs for branches in existingBranches.
// If existingBranches is nil, all branches are accepted.
func newMockGitHubAPIWithBranches(t *testing.T, defaultBranch string, existingBranches []string) *github.Client {
	t.Helper()

	branchAllowed := func(branch string) bool {
		return existingBranches == nil || slices.Contains(existingBranches, branch)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/testowner/testrepo" && r.Method == http.MethodGet:
			_, _ = fmt.Fprintf(w, `{"default_branch": %q}`, defaultBranch)
		case r.URL.Path == "/repos/testowner/testrepo/pulls" && r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, `[{"number": 1, "html_url": "https://github.com/testowner/testrepo/pull/1"}]`)
		case strings.HasPrefix(r.URL.Path, "/repos/testowner/testrepo/git/ref/heads/") && r.Method == http.MethodGet:
			branch := strings.TrimPrefix(r.URL.Path, "/repos/testowner/testrepo/git/ref/heads/")
			if branchAllowed(branch) {
				ref := map[string]any{
					"ref":    "refs/heads/" + branch,
					"object": map[string]string{"sha": "abc123", "type": "commit"},
				}
				_ = json.NewEncoder(w).Encode(ref)
			} else {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
			}
		default:
			t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parsing mock server URL: %v", err)
	}
	client.BaseURL = baseURL
	return client
}

func overrideGitHubClient(t *testing.T, client *github.Client) {
	t.Helper()
	orig := newGitHubClient
	t.Cleanup(func() { newGitHubClient = orig })
	newGitHubClient = func(_ context.Context, _ string) *github.Client {
		return client
	}
}
