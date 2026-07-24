package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DPS0340/kubeoptimizer/internal/check"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

func fixtures() (*snapshot.Snapshot, []check.Finding) {
	s := &snapshot.Snapshot{HasMetrics: false, Errors: []string{"jobs: forbidden"}}
	fs := []check.Finding{
		{CheckID: "unused-pv", Target: "pv/small", MonthlyCost: 10, Confidence: check.Certain},
		{CheckID: "idle-gpu", Target: "node/big", MonthlyCost: 2233.8, Confidence: check.Estimate},
	}
	return s, fs
}

func TestBuildSortsAndTotals(t *testing.T) {
	s, fs := fixtures()
	r := Build("https://cluster.example", s, fs)
	if r.Findings[0].CheckID != "idle-gpu" {
		t.Fatal("findings must be sorted by cost desc")
	}
	if r.TotalMonthlyUSD < 2243 || r.TotalMonthlyUSD > 2244 {
		t.Fatalf("total = %v, want ~2243.8", r.TotalMonthlyUSD)
	}
	joined := strings.Join(r.Notes, "\n")
	if !strings.Contains(joined, "metrics-server not detected") {
		t.Fatalf("notes must mention missing metrics: %v", r.Notes)
	}
	if !strings.Contains(joined, "jobs: forbidden") {
		t.Fatalf("notes must surface collection errors: %v", r.Notes)
	}
}

func TestBuildNamespaceNote(t *testing.T) {
	s := &snapshot.Snapshot{Namespace: "team-a", HasMetrics: true}
	r := Build("https://cluster.example", s, nil)
	joined := strings.Join(r.Notes, "\n")
	if !strings.Contains(joined, "limited to namespace team-a") {
		t.Fatalf("notes must flag the namespace filter: %v", r.Notes)
	}
	// unfiltered scan must not carry the note
	r2 := Build("https://cluster.example", &snapshot.Snapshot{HasMetrics: true}, nil)
	if strings.Contains(strings.Join(r2.Notes, "\n"), "limited to namespace") {
		t.Fatalf("unexpected namespace note: %v", r2.Notes)
	}
}

func TestRenderTable(t *testing.T) {
	s, fs := fixtures()
	var buf bytes.Buffer
	RenderTable(&buf, Build("https://cluster.example", s, fs))
	out := buf.String()
	for _, want := range []string{"idle-gpu", "node/big", "$2233.80", "TOTAL", "$2243.80"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	s, fs := fixtures()
	var buf bytes.Buffer
	if err := RenderJSON(&buf, Build("c", s, fs)); err != nil {
		t.Fatal(err)
	}
	var round Report
	if err := json.Unmarshal(buf.Bytes(), &round); err != nil {
		t.Fatalf("output must be valid JSON: %v", err)
	}
	if len(round.Findings) != 2 {
		t.Fatalf("roundtrip findings = %d", len(round.Findings))
	}
}
