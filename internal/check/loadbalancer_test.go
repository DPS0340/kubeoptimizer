package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

func TestLBCheck(t *testing.T) {
	lbType := func(name string) corev1.Service {
		return corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		}
	}
	ready := true
	notReady := false
	s := &snapshot.Snapshot{
		Services: []corev1.Service{
			lbType("idle-lb"),     // no slices at all → finding
			lbType("healthy-lb"),  // has a ready endpoint → no finding
			lbType("notready-lb"), // slice exists but nothing ready → finding
			lbType("unknown-lb"),  // Ready == nil counts as ready → no finding
			{ // ClusterIP → ignored
				ObjectMeta: metav1.ObjectMeta{Name: "clusterip", Namespace: "default"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
		},
		EndpointSlices: map[string][]discoveryv1.EndpointSlice{
			snapshot.Key("default", "healthy-lb"): {{Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			}}},
			snapshot.Key("default", "notready-lb"): {{Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
			}}},
			snapshot.Key("default", "unknown-lb"): {{Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.3"}},
			}}},
		},
	}
	fs := LBCheck{}.Run(s, testModel())
	if len(fs) != 2 {
		t.Fatalf("findings = %d, want 2, got %+v", len(fs), fs)
	}
	if fs[0].Target != "svc/default/idle-lb" || fs[0].Confidence != Certain {
		t.Fatalf("unexpected finding: %+v", fs[0])
	}
	if fs[1].Target != "svc/default/notready-lb" {
		t.Fatalf("unexpected finding: %+v", fs[1])
	}
	if fs[0].MonthlyCost != 18.0 {
		t.Fatalf("cost = %v, want 18.0", fs[0].MonthlyCost)
	}
}
