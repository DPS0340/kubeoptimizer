package check

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

func TestGPUCheck(t *testing.T) {
	gpu := corev1.ResourceName("nvidia.com/gpu")
	mkNode := func(name string, gpus string) corev1.Node {
		return corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name,
				Labels: map[string]string{"node.kubernetes.io/instance-type": "p3.2xlarge"}},
			Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{gpu: resource.MustParse(gpus)}},
		}
	}
	gpuPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "train", Namespace: "ml"},
		Spec: corev1.PodSpec{
			NodeName: "gpu-used",
			Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{gpu: resource.MustParse("1")}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	partialPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: "ml"},
		Spec: corev1.PodSpec{
			NodeName: "gpu-partial",
			Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{gpu: resource.MustParse("1")}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	s := &snapshot.Snapshot{
		Nodes: []corev1.Node{
			mkNode("gpu-idle", "1"),      // no gpu pods → full node cost finding
			mkNode("gpu-used", "1"),      // fully requested → no finding
			mkNode("gpu-partial", "2"),   // 1 of 2 GPUs requested → warning finding
		},
		Pods: []corev1.Pod{gpuPod, partialPod},
	}
	fs := GPUCheck{}.Run(s, testModel())
	if len(fs) != 2 {
		t.Fatalf("findings = %d, want 2, got %+v", len(fs), fs)
	}

	// Assert idle GPU node finding
	idleFinding := findByTarget(fs, "node/gpu-idle")
	if idleFinding == nil {
		t.Fatalf("missing finding for node/gpu-idle")
	}
	if idleFinding.MonthlyCost < 2233 || idleFinding.MonthlyCost > 2235 {
		t.Fatalf("idle node cost = %v, want ~2233.8", idleFinding.MonthlyCost)
	}
	if idleFinding.Confidence != Estimate {
		t.Fatalf("idle node confidence = %s, want Estimate", idleFinding.Confidence)
	}
	if !strings.HasPrefix(idleFinding.CostBasis, "whole node priced") {
		t.Fatalf("idle node CostBasis must start with 'whole node priced'; got %q", idleFinding.CostBasis)
	}

	// Assert partial GPU node finding
	partialFinding := findByTarget(fs, "node/gpu-partial")
	if partialFinding == nil {
		t.Fatalf("missing finding for node/gpu-partial")
	}
	if partialFinding.MonthlyCost != 0 {
		t.Fatalf("partial node cost = %v, want 0", partialFinding.MonthlyCost)
	}
	if partialFinding.Confidence != Estimate {
		t.Fatalf("partial node confidence = %s, want Estimate", partialFinding.Confidence)
	}
	if partialFinding.Reason != "1 of 2 GPU(s) unrequested" {
		t.Fatalf("partial node reason = %q, want '1 of 2 GPU(s) unrequested'", partialFinding.Reason)
	}

	// Verify no finding for fully-used node
	if usedFinding := findByTarget(fs, "node/gpu-used"); usedFinding != nil {
		t.Fatalf("node/gpu-used should have no finding, got %+v", usedFinding)
	}
}
