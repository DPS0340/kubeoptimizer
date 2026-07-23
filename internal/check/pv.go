package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

// PVCheck finds storage that is billed but serves no workload:
// Released/Available PVs and bound PVCs no pod mounts.
type PVCheck struct{}

func (PVCheck) ID() string { return "unused-pv" }

func (PVCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, pv := range s.PVs {
		if pv.Status.Phase != corev1.VolumeReleased && pv.Status.Phase != corev1.VolumeAvailable {
			continue
		}
		capQ := pv.Spec.Capacity[corev1.ResourceStorage]
		usd, basis := m.PVMonthlyUSD(capQ.Value())
		conf := Certain
		if pv.Status.Phase == corev1.VolumeAvailable {
			conf = Estimate // static/local PVs may not be billed
		}
		out = append(out, Finding{
			CheckID:     "unused-pv",
			Target:      "pv/" + pv.Name,
			Reason:      fmt.Sprintf("PersistentVolume is %s — not bound to any claim", pv.Status.Phase),
			MonthlyCost: usd,
			CostBasis:   basis,
			Confidence:  conf,
			Action:      "verify data is not needed, then: kubectl delete pv " + pv.Name,
		})
	}

	mounted := map[string]bool{}
	for _, p := range s.Pods {
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				mounted[snapshot.Key(p.Namespace, v.PersistentVolumeClaim.ClaimName)] = true
			}
		}
	}
	for _, pvc := range s.PVCs {
		if pvc.Status.Phase != corev1.ClaimBound || mounted[snapshot.Key(pvc.Namespace, pvc.Name)] {
			continue
		}
		capQ := pvc.Status.Capacity[corev1.ResourceStorage]
		usd, basis := m.PVMonthlyUSD(capQ.Value())
		out = append(out, Finding{
			CheckID:     "unused-pv",
			Target:      fmt.Sprintf("pvc/%s/%s", pvc.Namespace, pvc.Name),
			Reason:      "PVC is bound but not mounted by any pod",
			MonthlyCost: usd,
			CostBasis:   basis,
			Confidence:  Estimate, // may be intentionally kept (backups etc.)
			Action:      fmt.Sprintf("verify, then: kubectl -n %s delete pvc %s", pvc.Namespace, pvc.Name),
		})
	}
	return out
}
