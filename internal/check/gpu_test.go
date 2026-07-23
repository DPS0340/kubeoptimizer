package check

import (
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
	s := &snapshot.Snapshot{
		Nodes: []corev1.Node{
			mkNode("gpu-idle", "1"), // no gpu pods → full node cost finding
			mkNode("gpu-used", "1"), // fully requested → no finding
		},
		Pods: []corev1.Pod{gpuPod},
	}
	fs := GPUCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	// p3.2xlarge: 3.06 * 730 = 2233.8
	if fs[0].Target != "node/gpu-idle" || fs[0].MonthlyCost < 2233 || fs[0].MonthlyCost > 2235 {
		t.Fatalf("unexpected: %+v", fs[0])
	}
}
