package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Status represents the version check status for a chart.
type Status string

const (
	StatusUpToDate       Status = "UP TO DATE"
	StatusUpdateAvailable Status = "UPDATE AVAILABLE"
	StatusBreaking       Status = "BREAKING (major)"
	StatusError          Status = "ERROR"
)

// DriftResult represents the version check result for a single chart reference.
type DriftResult struct {
	ChartName      string `json:"chartName"`
	FilePath       string `json:"filePath"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Status         Status `json:"status"`
	RepoURL        string `json:"repoURL"`
	SourceType     string `json:"sourceType"`
}

// Renderer outputs drift results in various formats.
type Renderer struct {
	Writer      io.Writer
	ShowUpToDate bool
}

// filterResults returns results filtered by ShowUpToDate setting.
func (r *Renderer) filterResults(results []DriftResult) []DriftResult {
	if r.ShowUpToDate {
		return results
	}
	filtered := make([]DriftResult, 0, len(results))
	for _, res := range results {
		if res.Status != StatusUpToDate {
			filtered = append(filtered, res)
		}
	}
	return filtered
}

// RenderTable outputs results as an aligned table.
func (r *Renderer) RenderTable(results []DriftResult) {
	w := tabwriter.NewWriter(r.Writer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CHART\tFILE\tCURRENT\tLATEST\tSTATUS")

	for _, res := range results {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			res.ChartName,
			res.FilePath,
			res.CurrentVersion,
			res.LatestVersion,
			res.Status,
		)
	}
	w.Flush()
}

// RenderJSON outputs results as JSON.
func (r *Renderer) RenderJSON(results []DriftResult) error {
	enc := json.NewEncoder(r.Writer)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// RenderMarkdown outputs results as a Markdown table.
func (r *Renderer) RenderMarkdown(results []DriftResult) {
	fmt.Fprintln(r.Writer, "| Chart | File | Current | Latest | Status |")
	fmt.Fprintln(r.Writer, "|-------|------|---------|--------|--------|")

	for _, res := range results {
		fmt.Fprintf(r.Writer, "| %s | %s | %s | %s | %s |\n",
			res.ChartName,
			res.FilePath,
			res.CurrentVersion,
			res.LatestVersion,
			res.Status,
		)
	}
}

// Render dispatches to the appropriate format renderer.
func (r *Renderer) Render(results []DriftResult, format string) error {
	filtered := r.filterResults(results)
	switch strings.ToLower(format) {
	case "json":
		return r.RenderJSON(filtered)
	case "markdown", "md":
		r.RenderMarkdown(filtered)
		return nil
	default:
		r.RenderTable(filtered)
		return nil
	}
}

// Summary returns a human-readable summary of the results.
func Summary(results []DriftResult) string {
	var updates, breaking, upToDate, errors int
	for _, res := range results {
		switch res.Status {
		case StatusUpToDate:
			upToDate++
		case StatusUpdateAvailable:
			updates++
		case StatusBreaking:
			breaking++
		case StatusError:
			errors++
		}
	}
	parts := []string{fmt.Sprintf("%d chart(s) scanned", len(results))}
	if updates > 0 {
		parts = append(parts, fmt.Sprintf("%d update(s) available", updates))
	}
	if breaking > 0 {
		parts = append(parts, fmt.Sprintf("%d breaking update(s)", breaking))
	}
	if upToDate > 0 {
		parts = append(parts, fmt.Sprintf("%d up to date", upToDate))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s)", errors))
	}
	return strings.Join(parts, ", ")
}
