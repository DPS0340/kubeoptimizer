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

func findByTarget(fs []Finding, target string) *Finding {
	for i, f := range fs {
		if f.Target == target {
			return &fs[i]
		}
	}
	return nil
}

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
			{ // Available → estimate finding
				ObjectMeta: metav1.ObjectMeta{Name: "pv-available"},
				Spec: corev1.PersistentVolumeSpec{Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("20Gi")}},
				Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeAvailable},
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
	if len(fs) != 3 {
		t.Fatalf("findings = %d, want 3 (released PV + available PV + orphan PVC), got %+v", len(fs), fs)
	}

	// Assert released PV finding
	releasedPV := findByTarget(fs, "pv/pv-released")
	if releasedPV == nil {
		t.Fatalf("missing finding for pv/pv-released")
	}
	if releasedPV.Confidence != Certain {
		t.Fatalf("released PV must be Certain, got %s", releasedPV.Confidence)
	}
	if releasedPV.MonthlyCost < 9.9 || releasedPV.MonthlyCost > 10.1 { // 100Gi × $0.10
		t.Fatalf("released PV cost = %v, want ~10", releasedPV.MonthlyCost)
	}
	if releasedPV.CostBasis == "" {
		t.Fatalf("released PV must have CostBasis")
	}

	// Assert available PV finding
	availablePV := findByTarget(fs, "pv/pv-available")
	if availablePV == nil {
		t.Fatalf("missing finding for pv/pv-available")
	}
	if availablePV.Confidence != Estimate {
		t.Fatalf("available PV must be Estimate, got %s", availablePV.Confidence)
	}
	if availablePV.MonthlyCost < 1.9 || availablePV.MonthlyCost > 2.1 { // 20Gi × $0.10
		t.Fatalf("available PV cost = %v, want ~2", availablePV.MonthlyCost)
	}
	if availablePV.CostBasis == "" {
		t.Fatalf("available PV must have CostBasis")
	}

	// Assert orphan PVC finding
	orphanPVC := findByTarget(fs, "pvc/default/orphan")
	if orphanPVC == nil {
		t.Fatalf("missing finding for pvc/default/orphan")
	}
	if orphanPVC.Confidence != Estimate {
		t.Fatalf("orphan PVC must be Estimate, got %s", orphanPVC.Confidence)
	}
	if orphanPVC.MonthlyCost < 4.9 || orphanPVC.MonthlyCost > 5.1 { // 50Gi × $0.10
		t.Fatalf("orphan PVC cost = %v, want ~5", orphanPVC.MonthlyCost)
	}
	if orphanPVC.CostBasis == "" {
		t.Fatalf("orphan PVC must have CostBasis")
	}
}
