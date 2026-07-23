package snapshot

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestCollectGathersResources(t *testing.T) {
	kube := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
		&corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
	)
	s := Collect(context.Background(), kube, nil)
	if len(s.Nodes) != 1 || len(s.Pods) != 1 || len(s.Services) != 1 {
		t.Fatalf("unexpected counts: nodes=%d pods=%d svcs=%d", len(s.Nodes), len(s.Pods), len(s.Services))
	}
	if s.Endpoints[Key("default", "s1")] == nil {
		t.Fatal("endpoints not indexed by ns/name")
	}
	if s.HasMetrics {
		t.Fatal("HasMetrics must be false when metrics client is nil")
	}
}

func TestCollectIsolatesFailures(t *testing.T) {
	kube := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}})
	kube.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	s := Collect(context.Background(), kube, nil)
	if len(s.Nodes) != 1 {
		t.Fatal("node collection must survive pod list failure")
	}
	if len(s.Errors) == 0 {
		t.Fatal("pod list failure must be recorded in Errors")
	}
}

func TestCollectMetrics(t *testing.T) {
	kube := fake.NewSimpleClientset()
	pm := &metricsv1beta1.PodMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Containers: []metricsv1beta1.ContainerMetrics{{
			Name: "c1",
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}},
	}
	mc := metricsfake.NewSimpleClientset()
	mc.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1.PodMetricsList{Items: []metricsv1beta1.PodMetrics{*pm}}, nil
	})
	s := Collect(context.Background(), kube, mc)
	if !s.HasMetrics {
		t.Fatal("HasMetrics must be true")
	}
	u := s.PodUsage[Key("default", "p1")]
	if u.CPUMilli != 250 || u.MemBytes != 512*1024*1024 {
		t.Fatalf("usage = %+v", u)
	}
}
