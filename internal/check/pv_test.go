package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

func testModel() *cost.Model { return cost.NewModel(cost.DefaultRates()) }

func TestPVCheck(t *testing.T) {
	s := &snapshot.Snapshot{
		PVs: []corev1.PersistentVolume{
			{ // Released → certain finding
				ObjectMeta: metav1.ObjectMeta{Name: "pv-released"},
				Spec: corev1.PersistentVolumeSpec{Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi")}},
				Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeReleased},
			},
			{ // Bound → no finding
				ObjectMeta: metav1.ObjectMeta{Name: "pv-bound"},
				Spec: corev1.PersistentVolumeSpec{Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi")}},
				Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
			},
		},
		PVCs: []corev1.PersistentVolumeClaim{
			{ // bound but no pod mounts it → orphan finding
				ObjectMeta: metav1.ObjectMeta{Name: "orphan", Namespace: "default"},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase:    corev1.ClaimBound,
					Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("50Gi")},
				},
			},
			{ // bound and mounted → no finding
				ObjectMeta: metav1.ObjectMeta{Name: "used", Namespace: "default"},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase:    corev1.ClaimBound,
					Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")},
				},
			},
		},
		Pods: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "used"}},
			}}},
		}},
	}
	fs := PVCheck{}.Run(s, testModel())
	if len(fs) != 2 {
		t.Fatalf("findings = %d, want 2 (released PV + orphan PVC), got %+v", len(fs), fs)
	}
	if fs[0].Confidence != Certain {
		t.Fatalf("released PV must be certain, got %s", fs[0].Confidence)
	}
	if fs[0].MonthlyCost < 9.9 || fs[0].MonthlyCost > 10.1 { // 100Gi × $0.10
		t.Fatalf("released PV cost = %v, want ~10", fs[0].MonthlyCost)
	}
}
