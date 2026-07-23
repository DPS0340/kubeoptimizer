package cost

import (
	"math"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestRequestsMonthlyUSD(t *testing.T) {
	m := NewModel(DefaultRates())
	// 1 vCPU + 1 GiB: (0.0316 + 0.0042) * 730 = 26.134
	usd, basis := m.RequestsMonthlyUSD(1000, GiB)
	if !almostEqual(usd, 26.134) {
		t.Fatalf("usd = %v, want ~26.134", usd)
	}
	if basis == "" {
		t.Fatal("basis must not be empty")
	}
}

func TestNodeMonthlyUSD_KnownInstanceType(t *testing.T) {
	m := NewModel(DefaultRates())
	n := corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{"node.kubernetes.io/instance-type": "m5.xlarge"},
	}}
	usd, basis := m.NodeMonthlyUSD(n)
	// 0.192 * 730 = 140.16
	if !almostEqual(usd, 140.16) {
		t.Fatalf("usd = %v, want ~140.16", usd)
	}
	if basis == "" {
		t.Fatal("basis must not be empty")
	}
}

func TestNodeMonthlyUSD_UnknownFallsBackToAllocatable(t *testing.T) {
	m := NewModel(DefaultRates())
	n := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node.kubernetes.io/instance-type": "weird.type"}},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		}},
	}
	usd, _ := m.NodeMonthlyUSD(n)
	// (2*0.0316 + 8*0.0042) * 730 = 70.664
	if !almostEqual(usd, 70.664) {
		t.Fatalf("usd = %v, want ~70.664", usd)
	}
}

func TestPVAndLB(t *testing.T) {
	m := NewModel(DefaultRates())
	usd, _ := m.PVMonthlyUSD(100 * GiB) // 100 GiB * $0.10 = $10
	if !almostEqual(usd, 10.0) {
		t.Fatalf("pv usd = %v, want 10.0", usd)
	}
	lb, _ := m.LBMonthly()
	if !almostEqual(lb, 18.0) {
		t.Fatalf("lb usd = %v, want 18.0", lb)
	}
}
