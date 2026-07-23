package check

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

func rsPod(name, rsName, cpu, mem string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "prod",
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rsName}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(mem),
			}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestRightsizingSkipsWithoutMetrics(t *testing.T) {
	s := &snapshot.Snapshot{
		HasMetrics: false,
		Pods:       []corev1.Pod{rsPod("a-x1-p", "a-x1", "2", "4Gi")},
	}
	fs := RightsizingCheck{}.Run(s, testModel())
	if len(fs) != 0 {
		t.Fatalf("expected no findings without metrics, got %+v", fs)
	}
}

func TestRightsizingFindsOverprovisioned(t *testing.T) {
	// requests: 2 vCPU / 4Gi, usage: 200m / 512Mi → way over
	s := &snapshot.Snapshot{
		HasMetrics: true,
		Pods:       []corev1.Pod{rsPod("web-abc12-p1", "web-abc12", "2", "4Gi")},
		PodUsage: map[string]snapshot.PodUsage{
			snapshot.Key("prod", "web-abc12-p1"): {CPUMilli: 200, MemBytes: 512 * MiB},
		},
	}
	fs := RightsizingCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	f := fs[0]
	if f.Target != "prod/ReplicaSet/web" || f.Confidence != Estimate {
		t.Fatalf("unexpected: %+v", f)
	}
	if f.MonthlyCost <= 0 {
		t.Fatalf("expected positive savings, got %v", f.MonthlyCost)
	}
	if !strings.Contains(f.CostBasis, "point-in-time") {
		t.Fatalf("basis must disclose rough sampling: %q", f.CostBasis)
	}
}

func TestRightsizingIgnoresWellSized(t *testing.T) {
	// usage 80% of request → no finding
	s := &snapshot.Snapshot{
		HasMetrics: true,
		Pods:       []corev1.Pod{rsPod("ok-abc12-p1", "ok-abc12", "1", "1Gi")},
		PodUsage: map[string]snapshot.PodUsage{
			snapshot.Key("prod", "ok-abc12-p1"): {CPUMilli: 800, MemBytes: 820 * MiB},
		},
	}
	fs := RightsizingCheck{}.Run(s, testModel())
	if len(fs) != 0 {
		t.Fatalf("expected no findings, got %+v", fs)
	}
}
