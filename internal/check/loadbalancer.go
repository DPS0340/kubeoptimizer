package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

// LBCheck finds LoadBalancer services with zero ready endpoints:
// the cloud LB is billed hourly while routing traffic to nothing.
type LBCheck struct{}

func (LBCheck) ID() string { return "idle-loadbalancer" }

func (LBCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, svc := range s.Services {
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		ready := 0
		if ep := s.Endpoints[snapshot.Key(svc.Namespace, svc.Name)]; ep != nil {
			for _, sub := range ep.Subsets {
				ready += len(sub.Addresses)
			}
		}
		if ready > 0 {
			continue
		}
		usd, basis := m.LBMonthly()
		out = append(out, Finding{
			CheckID:     "idle-loadbalancer",
			Target:      fmt.Sprintf("svc/%s/%s", svc.Namespace, svc.Name),
			Reason:      "LoadBalancer service has no ready endpoints — cloud LB billed for nothing",
			MonthlyCost: usd,
			CostBasis:   basis,
			Confidence:  Certain,
			Action:      fmt.Sprintf("fix selector or delete: kubectl -n %s delete svc %s", svc.Namespace, svc.Name),
		})
	}
	return out
}
