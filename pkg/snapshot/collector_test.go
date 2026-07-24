package snapshot

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		&discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
			Name: "s1-abc123", Namespace: "default",
			Labels: map[string]string{discoveryv1.LabelServiceName: "s1"},
		}},
		&discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
			Name: "custom-no-label", Namespace: "default",
		}},
	)
	s := Collect(context.Background(), kube, nil, "")
	if len(s.Nodes) != 1 || len(s.Pods) != 1 || len(s.Services) != 1 {
		t.Fatalf("unexpected counts: nodes=%d pods=%d svcs=%d", len(s.Nodes), len(s.Pods), len(s.Services))
	}
	if len(s.EndpointSlices[Key("default", "s1")]) != 1 {
		t.Fatal("endpointslices not indexed by ns/serviceName")
	}
	if len(s.EndpointSlices) != 1 {
		t.Fatalf("slices without the service-name label must be skipped, got %+v", s.EndpointSlices)
	}
	if s.HasMetrics {
		t.Fatal("HasMetrics must be false when metrics client is nil")
	}
}

func TestCollectNamespaceScoped(t *testing.T) {
	kube := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
		&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv1"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "team-a"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "team-b"}},
	)
	s := Collect(context.Background(), kube, nil, "team-a")
	if s.Namespace != "team-a" {
		t.Fatalf("Namespace = %q", s.Namespace)
	}
	if len(s.Nodes) != 0 || len(s.PVs) != 0 {
		t.Fatal("cluster-scoped resources must be skipped under a namespace filter")
	}
	if len(s.Pods) != 1 || s.Pods[0].Namespace != "team-a" {
		t.Fatalf("pods = %+v, want only team-a", s.Pods)
	}
	if len(s.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", s.Errors)
	}
}

func TestCollectIsolatesFailures(t *testing.T) {
	kube := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}})
	kube.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	s := Collect(context.Background(), kube, nil, "")
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
	s := Collect(context.Background(), kube, mc, "")
	if !s.HasMetrics {
		t.Fatal("HasMetrics must be true")
	}
	u := s.PodUsage[Key("default", "p1")]
	if u.CPUMilli != 250 || u.MemBytes != 512*1024*1024 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestCollectMetricsForbiddenSurfaces(t *testing.T) {
	kube := fake.NewSimpleClientset()
	mc := metricsfake.NewSimpleClientset()
	gr := schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}
	mc.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(gr, "", errors.New("no access"))
	})
	s := Collect(context.Background(), kube, mc, "")
	if s.HasMetrics {
		t.Fatal("HasMetrics must be false when metrics list is forbidden")
	}
	found := false
	for _, e := range s.Errors {
		if strings.Contains(e, "podmetrics") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Errors must contain a podmetrics entry, got %+v", s.Errors)
	}
}

func TestCollectMetricsNotFoundIsQuiet(t *testing.T) {
	kube := fake.NewSimpleClientset()
	mc := metricsfake.NewSimpleClientset()
	gr := schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}
	mc.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(gr, "")
	})
	s := Collect(context.Background(), kube, mc, "")
	if s.HasMetrics {
		t.Fatal("HasMetrics must be false when metrics API is not found")
	}
	if len(s.Errors) != 0 {
		t.Fatalf("NotFound must not add an error entry, got %+v", s.Errors)
	}
}
