package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderTable(t *testing.T) {
	results := []DriftResult{
		{ChartName: "cert-manager", FilePath: "apps/cm.yaml", CurrentVersion: "1.13.2", LatestVersion: "1.14.1", Status: StatusUpdateAvailable},
		{ChartName: "grafana", FilePath: "apps/grafana.yaml", CurrentVersion: "7.0.1", LatestVersion: "8.2.0", Status: StatusBreaking},
		{ChartName: "ingress-nginx", FilePath: "apps/ingress.yaml", CurrentVersion: "4.9.0", LatestVersion: "4.9.0", Status: StatusUpToDate},
	}

	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, ShowUpToDate: false}
	if err := r.Render(results, "table"); err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	if !strings.Contains(output, "cert-manager") {
		t.Error("expected cert-manager in output")
	}
	if !strings.Contains(output, "UPDATE AVAILABLE") {
		t.Error("expected UPDATE AVAILABLE in output")
	}
	if !strings.Contains(output, "BREAKING (major)") {
		t.Error("expected BREAKING (major) in output")
	}
	// Up-to-date should be hidden
	if strings.Contains(output, "UP TO DATE") {
		t.Error("did not expect UP TO DATE when ShowUpToDate is false")
	}
}

func TestRenderTable_ShowUpToDate(t *testing.T) {
	results := []DriftResult{
		{ChartName: "ingress-nginx", Status: StatusUpToDate},
	}

	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, ShowUpToDate: true}
	if err := r.Render(results, "table"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "UP TO DATE") {
		t.Error("expected UP TO DATE when ShowUpToDate is true")
	}
}

func TestRenderJSON(t *testing.T) {
	results := []DriftResult{
		{ChartName: "cert-manager", CurrentVersion: "1.13.2", LatestVersion: "1.14.1", Status: StatusUpdateAvailable},
	}

	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, ShowUpToDate: false}
	if err := r.Render(results, "json"); err != nil {
		t.Fatal(err)
	}

	var parsed []DriftResult
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if len(parsed) != 1 || parsed[0].ChartName != "cert-manager" {
		t.Errorf("unexpected parsed output: %v", parsed)
	}
}

func TestRenderMarkdown(t *testing.T) {
	results := []DriftResult{
		{ChartName: "cert-manager", FilePath: "apps/cm.yaml", CurrentVersion: "1.13.2", LatestVersion: "1.14.1", Status: StatusUpdateAvailable},
	}

	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, ShowUpToDate: false}
	if err := r.Render(results, "markdown"); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "| cert-manager |") {
		t.Error("expected markdown table row")
	}
	if !strings.Contains(output, "|-------|") {
		t.Error("expected markdown header separator")
	}
}

func TestSummary(t *testing.T) {
	results := []DriftResult{
		{Status: StatusUpdateAvailable},
		{Status: StatusBreaking},
		{Status: StatusUpToDate},
		{Status: StatusError},
	}

	s := Summary(results)

	if !strings.Contains(s, "4 chart(s) scanned") {
		t.Errorf("expected 4 charts scanned in summary, got %s", s)
	}
	if !strings.Contains(s, "1 update(s) available") {
		t.Errorf("expected 1 update in summary, got %s", s)
	}
	if !strings.Contains(s, "1 breaking") {
		t.Errorf("expected 1 breaking in summary, got %s", s)
	}
}
