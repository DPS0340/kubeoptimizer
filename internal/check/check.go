package check

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

type Confidence string

const (
	Certain  Confidence = "certain"
	Estimate Confidence = "estimate"
)

const MiB = int64(1024 * 1024)

// Finding is one detected instance of waste, priced monthly with its
// derivation spelled out (CostBasis) — inflated numbers kill trust.
type Finding struct {
	CheckID     string     `json:"check"`
	Target      string     `json:"target"`
	Reason      string     `json:"reason"`
	MonthlyCost float64    `json:"monthly_cost_usd"`
	CostBasis   string     `json:"cost_basis,omitempty"`
	Confidence  Confidence `json:"confidence"`
	Action      string     `json:"action,omitempty"`
}

// Check: one rule = one file. Checks are pure over the snapshot.
type Check interface {
	ID() string
	Run(s *snapshot.Snapshot, m *cost.Model) []Finding
}

// All returns every registered check. Each check task appends here.
func All() []Check {
	return []Check{
		PVCheck{},
		LBCheck{},
	}
}

func podRequests(p corev1.Pod) (cpuMilli, memBytes int64) {
	for _, c := range p.Spec.Containers {
		if r, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuMilli += r.MilliValue()
		}
		if r, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			memBytes += r.Value()
		}
	}
	return
}

// groupKey groups pods by their top-level workload. ReplicaSet names
// carry a template hash suffix which we strip to get the Deployment name.
func groupKey(p corev1.Pod) string {
	if len(p.OwnerReferences) > 0 {
		o := p.OwnerReferences[0]
		name := o.Name
		if o.Kind == "ReplicaSet" {
			if i := strings.LastIndex(name, "-"); i > 0 {
				name = name[:i]
			}
		}
		return p.Namespace + "/" + o.Kind + "/" + name
	}
	return p.Namespace + "/Pod/" + p.Name
}

func fmtMem(b int64) string {
	return resource.NewQuantity(b, resource.BinarySI).String()
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
