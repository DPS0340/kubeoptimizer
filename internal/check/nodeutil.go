package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

const nodeUtilThresholdPct = 50

// NodeUtilCheck finds worker nodes where both CPU and memory requests
// sit under 50% of allocatable: consolidation candidates. Node-level
// findings carry the biggest single dollar amounts after GPUs.
type NodeUtilCheck struct{}

func (NodeUtilCheck) ID() string { return "underutilized-nodes" }

func (NodeUtilCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	reqCPU := map[string]int64{}
	reqMem := map[string]int64{}
	for _, p := range s.Pods {
		if p.Spec.NodeName == "" ||
			(p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending) {
			continue
		}
		c, mem := podRequests(p)
		reqCPU[p.Spec.NodeName] += c
		reqMem[p.Spec.NodeName] += mem
	}

	var out []Finding
	for _, n := range s.Nodes {
		if _, isCP := n.Labels["node-role.kubernetes.io/control-plane"]; isCP {
			continue
		}
		allocCPU := n.Status.Allocatable.Cpu().MilliValue()
		allocMem := n.Status.Allocatable.Memory().Value()
		if allocCPU == 0 || allocMem == 0 {
			continue
		}
		cpuPct := reqCPU[n.Name] * 100 / allocCPU
		memPct := reqMem[n.Name] * 100 / allocMem
		if cpuPct >= nodeUtilThresholdPct || memPct >= nodeUtilThresholdPct {
			continue
		}
		// idle GPU nodes are priced whole by GPUCheck — don't double-count
		if gpuCap, gpuReq := nodeGPU(s, n); gpuCap > 0 && gpuReq == 0 {
			continue
		}
		usd, basis := m.NodeMonthlyUSD(n)
		out = append(out, Finding{
			CheckID:     "underutilized-nodes",
			Target:      "node/" + n.Name,
			Reason:      fmt.Sprintf("requests at %d%% CPU / %d%% memory of allocatable — consolidation candidate", cpuPct, memPct),
			MonthlyCost: usd,
			CostBasis:   "full node price if drained away; " + basis,
			Confidence:  Estimate,
			Action:      "consider bin-packing via cluster-autoscaler/Karpenter consolidation, then remove the node",
		})
	}
	return out
}
