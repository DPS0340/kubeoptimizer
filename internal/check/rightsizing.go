package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

const (
	overprovisionPct = 40  // usage below this % of request = overprovisioned
	minSlackCPUMilli = 200 // ignore trivial slack
	minSlackMemBytes = 256 * MiB
	floorCPUMilli    = 50 // per-pod recommendation floors
	floorMemBytes    = 64 * MiB
)

// RightsizingCheck (rough tier): compares live metrics-server usage
// against requests per workload. One point-in-time sample — findings
// are estimates and say so. The paid tier replaces this with
// Prometheus p95/p99 over a window.
type RightsizingCheck struct{}

func (RightsizingCheck) ID() string { return "overprovisioned-requests" }

type rsAgg struct {
	reqCPU, reqMem, useCPU, useMem int64
	pods                           int64
}

func (RightsizingCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	if !s.HasMetrics {
		return nil
	}
	groups := map[string]*rsAgg{}
	for _, p := range s.Pods {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		u, ok := s.PodUsage[snapshot.Key(p.Namespace, p.Name)]
		if !ok {
			continue // no usage sample — exclude rather than guess
		}
		rc, rm := podRequests(p)
		if rc == 0 && rm == 0 {
			continue // covered by no-requests check
		}
		g := groups[groupKey(p)]
		if g == nil {
			g = &rsAgg{}
			groups[groupKey(p)] = g
		}
		g.reqCPU += rc
		g.reqMem += rm
		g.useCPU += u.CPUMilli
		g.useMem += u.MemBytes
		g.pods++
	}

	var out []Finding
	for key, g := range groups {
		overCPU := g.reqCPU >= minSlackCPUMilli && g.useCPU*100 < g.reqCPU*overprovisionPct
		overMem := g.reqMem >= minSlackMemBytes && g.useMem*100 < g.reqMem*overprovisionPct
		if !overCPU && !overMem {
			continue
		}
		recCPU, recMem := g.reqCPU, g.reqMem
		if overCPU {
			recCPU = max64(g.useCPU*3/2, floorCPUMilli*g.pods)
			if recCPU > g.reqCPU {
				recCPU = g.reqCPU // floor exceeds current request — nothing to save on this axis
			}
		}
		if overMem {
			recMem = max64(g.useMem*3/2, floorMemBytes*g.pods)
			if recMem > g.reqMem {
				recMem = g.reqMem // floor exceeds current request — nothing to save on this axis
			}
		}
		// If both dimensions clamped to their originals, skip finding (zero savings)
		if recCPU == g.reqCPU && recMem == g.reqMem {
			continue
		}
		usd, basis := m.RequestsMonthlyUSD(g.reqCPU-recCPU, g.reqMem-recMem)
		out = append(out, Finding{
			CheckID: "overprovisioned-requests",
			Target:  key,
			Reason: fmt.Sprintf("requests far above live usage (cpu %dm req vs %dm used, mem %s req vs %s used, %d pods)",
				g.reqCPU, g.useCPU, fmtMem(g.reqMem), fmtMem(g.useMem), g.pods),
			MonthlyCost: usd,
			CostBasis:   "rough point-in-time metrics-server sample (usage ×1.5 headroom); " + basis,
			Confidence:  Estimate,
			Action: fmt.Sprintf("reduce total requests to ~cpu %dm / mem %s, verify over a longer window first",
				recCPU, fmtMem(recMem)),
		})
	}
	return out
}
