package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

var gpuResource = corev1.ResourceName("nvidia.com/gpu")

// nodeGPU returns the node's GPU allocatable and the total GPUs
// requested by active (non-Succeeded/Failed) pods on it.
func nodeGPU(s *snapshot.Snapshot, n corev1.Node) (capacity, requested int64) {
	capQ, ok := n.Status.Allocatable[gpuResource]
	if !ok {
		return 0, 0
	}
	capacity = capQ.Value()
	for _, p := range s.Pods {
		if p.Spec.NodeName != n.Name ||
			p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, c := range p.Spec.Containers {
			if q, ok := c.Resources.Requests[gpuResource]; ok {
				requested += q.Value()
			}
		}
	}
	return capacity, requested
}

// GPUCheck finds GPU nodes whose GPUs nobody requested. GPU instances
// are the most expensive line item in most clusters; an idle one is
// the single biggest finding a scan can produce.
type GPUCheck struct{}

func (GPUCheck) ID() string { return "idle-gpu" }

func (GPUCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		capQ, ok := n.Status.Allocatable[gpuResource]
		if !ok || capQ.IsZero() {
			continue
		}
		_, requested := nodeGPU(s, n)
		switch {
		case requested == 0:
			usd, basis := m.NodeMonthlyUSD(n)
			out = append(out, Finding{
				CheckID:     "idle-gpu",
				Target:      "node/" + n.Name,
				Reason:      fmt.Sprintf("node has %s GPU(s), zero requested by any pod", capQ.String()),
				MonthlyCost: usd,
				CostBasis:   "whole node priced; " + basis,
				Confidence:  Estimate, // allocation-based, not utilization-based
				Action:      "drain and scale down the GPU node pool if no GPU workloads are planned",
			})
		case requested < capQ.Value():
			out = append(out, Finding{
				CheckID:    "idle-gpu",
				Target:     "node/" + n.Name,
				Reason:     fmt.Sprintf("%d of %s GPU(s) unrequested", capQ.Value()-requested, capQ.String()),
				Confidence: Estimate,
				Action:     "consolidate GPU workloads to free a whole node",
			})
		}
	}
	return out
}
