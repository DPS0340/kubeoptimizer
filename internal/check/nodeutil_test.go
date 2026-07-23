package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

func TestNodeUtilCheck(t *testing.T) {
	alloc := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("4"),
		corev1.ResourceMemory: resource.MustParse("16Gi"),
	}
	mkNode := func(name string, labels map[string]string) corev1.Node {
		return corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
			Status:     corev1.NodeStatus{Allocatable: alloc},
		}
	}
	mkPod := func(name, node, cpu, mem string) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(cpu),
					corev1.ResourceMemory: resource.MustParse(mem),
				}}}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	s := &snapshot.Snapshot{
		Nodes: []corev1.Node{
			mkNode("empty-node", nil),
			mkNode("busy-node", nil),
			mkNode("cp", map[string]string{"node-role.kubernetes.io/control-plane": ""}),
		},
		Pods: []corev1.Pod{
			mkPod("big", "busy-node", "3", "12Gi"),
			mkPod("tiny", "empty-node", "100m", "256Mi"),
		},
	}
	fs := NodeUtilCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	if fs[0].Target != "node/empty-node" {
		t.Fatalf("unexpected target: %s", fs[0].Target)
	}
}
