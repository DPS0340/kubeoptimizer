package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

func TestNoRequestsCheck(t *testing.T) {
	mkPod := func(name, rsName string, withRequests bool) corev1.Pod {
		c := corev1.Container{Name: "app"}
		if withRequests {
			c.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			}}
		}
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rsName}},
			},
			Spec:   corev1.PodSpec{Containers: []corev1.Container{c}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	s := &snapshot.Snapshot{Pods: []corev1.Pod{
		mkPod("bad-abc12-x1", "bad-abc12", false),
		mkPod("bad-abc12-x2", "bad-abc12", false), // same group → dedupe to 1
		mkPod("good-def34-y1", "good-def34", true),
	}}
	fs := NoRequestsCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1 (deduped per workload), got %+v", len(fs), fs)
	}
	if fs[0].Target != "default/ReplicaSet/bad" || fs[0].MonthlyCost != 0 {
		t.Fatalf("unexpected: %+v", fs[0])
	}
}
