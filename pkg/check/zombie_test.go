package check

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func TestZombieCheck(t *testing.T) {
	now := time.Now()
	old := metav1.NewTime(now.Add(-96 * time.Hour))      // 4d
	week := metav1.NewTime(now.Add(-8 * 24 * time.Hour)) // 8d
	fresh := metav1.NewTime(now.Add(-1 * time.Hour))

	crashPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "crash", Namespace: "default", CreationTimestamp: old},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}},
		}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}},
		},
	}
	freshCrash := *crashPod.DeepCopy()
	freshCrash.Name = "fresh-crash"
	freshCrash.CreationTimestamp = fresh

	failedPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: "default", CreationTimestamp: old},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}
	donePod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: "default", CreationTimestamp: week},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	doneJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "old-job", Namespace: "default"},
		Status:     batchv1.JobStatus{CompletionTime: &week},
	}

	s := &snapshot.Snapshot{
		CollectedAt: now,
		Pods:        []corev1.Pod{crashPod, freshCrash, failedPod, donePod},
		Jobs:        []batchv1.Job{doneJob},
	}
	fs := ZombieCheck{}.Run(s, testModel())
	// crash(1) + failed(1) + succeeded(1) + job(1) = 4; fresh-crash excluded
	if len(fs) != 4 {
		t.Fatalf("findings = %d, want 4, got %+v", len(fs), fs)
	}
	var crashCost float64
	for _, f := range fs {
		if f.Target == "pod/default/crash" {
			crashCost = f.MonthlyCost
		}
	}
	// 1 vCPU + 1 GiB reserved: ~26.13/mo
	if crashCost < 26 || crashCost > 27 {
		t.Fatalf("crashloop pod cost = %v, want ~26.13", crashCost)
	}
}
