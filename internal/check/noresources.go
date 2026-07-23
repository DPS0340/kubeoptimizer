package check

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

// NoRequestsCheck flags workloads whose containers set no resource
// requests: their cost is unschedulable/unpredictable and they distort
// node utilization math. Warning only — no dollar amount.
type NoRequestsCheck struct{}

func (NoRequestsCheck) ID() string { return "no-requests" }

func (NoRequestsCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, p := range s.Pods {
		if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending {
			continue
		}
		missing := false
		for _, c := range p.Spec.Containers {
			_, hasCPU := c.Resources.Requests[corev1.ResourceCPU]
			_, hasMem := c.Resources.Requests[corev1.ResourceMemory]
			if !hasCPU || !hasMem {
				missing = true
				break
			}
		}
		if !missing {
			continue
		}
		key := groupKey(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Finding{
			CheckID:    "no-requests",
			Target:     key,
			Reason:     "containers without CPU/memory requests — cost unpredictable, scheduling unprotected",
			Confidence: Certain,
			Action:     "set resources.requests on every container",
		})
	}
	return out
}
