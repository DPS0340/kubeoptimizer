package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodRequests(t *testing.T) {
	p := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		}}},
		{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("250m"),
		}}},
	}}}
	cpu, mem := podRequests(p)
	if cpu != 750 || mem != 256*MiB {
		t.Fatalf("cpu=%d mem=%d", cpu, mem)
	}
}

func TestGroupKeyStripsReplicaSetHash(t *testing.T) {
	p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace:       "prod",
		Name:            "web-7d9f8b6c5-x2k4j",
		OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-7d9f8b6c5"}},
	}}
	if got := groupKey(p); got != "prod/ReplicaSet/web" {
		t.Fatalf("groupKey = %q", got)
	}
	bare := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "solo"}}
	if got := groupKey(bare); got != "prod/Pod/solo" {
		t.Fatalf("groupKey bare = %q", got)
	}
}
