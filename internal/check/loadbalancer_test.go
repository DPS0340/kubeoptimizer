package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
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
	s := &snapshot.Snapshot{
		Services: []corev1.Service{
			lbType("idle-lb"),    // no endpoints object at all → finding
			lbType("healthy-lb"), // has ready addresses → no finding
			{ // ClusterIP → ignored
				ObjectMeta: metav1.ObjectMeta{Name: "clusterip", Namespace: "default"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
		},
		Endpoints: map[string]*corev1.Endpoints{
			snapshot.Key("default", "healthy-lb"): {Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{IP: "10.0.0.1"}},
			}}},
		},
	}
	fs := LBCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	if fs[0].Target != "svc/default/idle-lb" || fs[0].Confidence != Certain {
		t.Fatalf("unexpected finding: %+v", fs[0])
	}
	if fs[0].MonthlyCost != 18.0 {
		t.Fatalf("cost = %v, want 18.0", fs[0].MonthlyCost)
	}
}
