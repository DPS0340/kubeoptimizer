package check

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
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

// kubectlResource maps a workload owner kind (as encoded in groupKey)
// to the resource ref `kubectl set resources` accepts. ReplicaSet
// groups already carry the Deployment name (template hash stripped).
func kubectlResource(kind string) (string, bool) {
	switch kind {
	case "ReplicaSet", "Deployment":
		return "deployment", true
	case "StatefulSet":
		return "statefulset", true
	case "DaemonSet":
		return "daemonset", true
	default: // bare Pods, Jobs, custom owners: no safe in-place edit
		return "", false
	}
}

// rsAction renders a copy-pasteable command when the workload kind
// supports it, falling back to prose. The Helm/GitOps caveat matters:
// a direct edit to chart-managed resources is reverted on next sync,
// so the numbers double as reference values for the chart.
func rsAction(key string, pods, cpuMilli, memBytes int64) string {
	perCPU := (cpuMilli + pods - 1) / pods
	perMem := (memBytes/pods + MiB - 1) / MiB * MiB // round up to MiB for a clean flag value
	parts := strings.SplitN(key, "/", 3)
	if len(parts) == 3 {
		if res, ok := kubectlResource(parts[1]); ok {
			return fmt.Sprintf(
				"kubectl -n %s set resources %s/%s --requests=cpu=%dm,memory=%s (per pod; verify over a longer window first). Helm/GitOps-managed? Direct edits get reverted — apply these values in the chart instead",
				parts[0], res, parts[2], perCPU, fmtMem(perMem))
		}
	}
	return fmt.Sprintf("reduce requests to ~cpu %dm / mem %s per pod, verify over a longer window first; if Helm/GitOps-managed, apply in the chart",
		perCPU, fmtMem(perMem))
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
			Action:      rsAction(key, g.pods, recCPU, recMem),
		})
	}
	return out
}
