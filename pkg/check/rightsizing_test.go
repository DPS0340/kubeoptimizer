package check

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
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

func TestRightsizingClampsFloorToRequest(t *testing.T) {
	// 6 pods × 40m CPU + 300Mi mem request (group: 240m/1800Mi)
	// usage: 60m CPU + 500Mi mem
	// CPU: floor 300m exceeds request 240m → clamps to 240m (no saving)
	// Mem: rec = max(750Mi, 384Mi) = 750Mi < 1800Mi (real saving)
	pods := make([]corev1.Pod, 6)
	usage := make(map[string]snapshot.PodUsage)
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("small-x1-p%d", i)
		pods[i] = rsPod(name, "small-x1", "40m", "300Mi")
		usage[snapshot.Key("prod", name)] = snapshot.PodUsage{CPUMilli: 10, MemBytes: 83 * MiB}
	}
	s := &snapshot.Snapshot{
		HasMetrics: true,
		Pods:       pods,
		PodUsage:   usage,
	}
	fs := RightsizingCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	f := fs[0]
	if f.MonthlyCost <= 0 {
		t.Fatalf("expected positive savings, got %v", f.MonthlyCost)
	}
	if !strings.Contains(f.Action, "240m") {
		t.Fatalf("Action should recommend CPU 240m (clamped to request), got: %q", f.Action)
	}
}

func TestRightsizingMixedDimension(t *testing.T) {
	// Single pod: 2 CPU + 300Mi request, usage 100m CPU + 280Mi mem
	// CPU over (100m < 2000m×40%), mem NOT over (280Mi > 300Mi×40% = 120Mi, and 280/300=93%)
	// Assert: 1 finding, CPU reduced, mem kept at 300Mi original
	s := &snapshot.Snapshot{
		HasMetrics: true,
		Pods:       []corev1.Pod{rsPod("mixed-abc12-p1", "mixed-abc12", "2", "300Mi")},
		PodUsage: map[string]snapshot.PodUsage{
			snapshot.Key("prod", "mixed-abc12-p1"): {CPUMilli: 100, MemBytes: 280 * MiB},
		},
	}
	fs := RightsizingCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	f := fs[0]
	if f.MonthlyCost <= 0 {
		t.Fatalf("expected positive savings, got %v", f.MonthlyCost)
	}
	if !strings.Contains(f.Action, "300Mi") {
		t.Fatalf("Action should keep mem at 300Mi (non-over axis), got: %q", f.Action)
	}
}
