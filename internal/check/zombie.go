package check

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

const (
	crashLoopMinAge  = 72 * time.Hour
	staleFinishedAge = 7 * 24 * time.Hour
)

// ZombieCheck finds workloads that consume or clutter without serving:
// long-crashing pods still reserve their requests on a node (real cost);
// old Failed/Succeeded pods and finished Jobs are hygiene findings ($0).
type ZombieCheck struct{}

func (ZombieCheck) ID() string { return "zombie-workloads" }

func (ZombieCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, p := range s.Pods {
		age := s.CollectedAt.Sub(p.CreationTimestamp.Time)
		target := fmt.Sprintf("pod/%s/%s", p.Namespace, p.Name)

		if isCrashLooping(p) && age > crashLoopMinAge {
			cpu, mem := podRequests(p)
			usd, basis := m.RequestsMonthlyUSD(cpu, mem)
			out = append(out, Finding{
				CheckID:     "zombie-workloads",
				Target:      target,
				Reason:      fmt.Sprintf("CrashLoopBackOff for %dd — requests stay reserved on the node", int(age.Hours()/24)),
				MonthlyCost: usd,
				CostBasis:   basis,
				Confidence:  Estimate,
				Action:      "fix the crash or scale the workload to zero",
			})
			continue
		}
		if p.Status.Phase == corev1.PodFailed && age > crashLoopMinAge {
			out = append(out, Finding{
				CheckID: "zombie-workloads", Target: target,
				Reason:     fmt.Sprintf("Failed pod lingering for %dd", int(age.Hours()/24)),
				Confidence: Certain,
				Action:     fmt.Sprintf("kubectl -n %s delete pod %s", p.Namespace, p.Name),
			})
		}
		if p.Status.Phase == corev1.PodSucceeded && age > staleFinishedAge {
			out = append(out, Finding{
				CheckID: "zombie-workloads", Target: target,
				Reason:     fmt.Sprintf("Succeeded pod lingering for %dd", int(age.Hours()/24)),
				Confidence: Certain,
				Action:     fmt.Sprintf("kubectl -n %s delete pod %s", p.Namespace, p.Name),
			})
		}
	}
	for _, j := range s.Jobs {
		if j.Status.CompletionTime == nil || j.Spec.TTLSecondsAfterFinished != nil {
			continue
		}
		age := s.CollectedAt.Sub(j.Status.CompletionTime.Time)
		if age > staleFinishedAge {
			out = append(out, Finding{
				CheckID:    "zombie-workloads",
				Target:     fmt.Sprintf("job/%s/%s", j.Namespace, j.Name),
				Reason:     fmt.Sprintf("Job finished %dd ago, no ttlSecondsAfterFinished", int(age.Hours()/24)),
				Confidence: Certain,
				Action:     "set ttlSecondsAfterFinished on the Job spec, or delete it",
			})
		}
	}
	return out
}

func isCrashLooping(p corev1.Pod) bool {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			return true
		}
	}
	return false
}
