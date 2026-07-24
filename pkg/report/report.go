package report

import (
	"sort"
	"time"

	"github.com/DPS0340/kubeoptimizer/pkg/check"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

type Report struct {
	Cluster         string          `json:"cluster"`
	CollectedAt     time.Time       `json:"collected_at"`
	TotalMonthlyUSD float64         `json:"total_monthly_usd"`
	Findings        []check.Finding `json:"findings"`
	Notes           []string        `json:"notes,omitempty"`
}

// Build sorts findings by monthly cost (desc), totals them, and turns
// skipped data sources / collection failures into visible notes —
// never silent.
func Build(cluster string, s *snapshot.Snapshot, findings []check.Finding) Report {
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].MonthlyCost > findings[j].MonthlyCost
	})
	var total float64
	for _, f := range findings {
		total += f.MonthlyCost
	}
	var notes []string
	if s.Namespace != "" {
		notes = append(notes, "scan limited to namespace "+s.Namespace+
			" — cluster-scoped checks skipped (underutilized-nodes, idle-gpu, unused-pv on PVs)")
	}
	if !s.HasMetrics {
		notes = append(notes, "metrics-server not detected — usage-based checks skipped (right-sizing)")
	}
	for _, e := range s.Errors {
		notes = append(notes, "collection failed: "+e)
	}
	return Report{
		Cluster:         cluster,
		CollectedAt:     s.CollectedAt,
		TotalMonthlyUSD: total,
		Findings:        findings,
		Notes:           notes,
	}
}
