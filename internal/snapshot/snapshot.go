package snapshot

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// PodUsage is live usage summed over a pod's containers (metrics-server).
type PodUsage struct {
	CPUMilli int64
	MemBytes int64
}

// Snapshot is an immutable view of the cluster taken at one point in
// time. Checks read only from here — never from the API directly.
type Snapshot struct {
	CollectedAt time.Time
	Nodes       []corev1.Node
	Pods        []corev1.Pod
	PVs         []corev1.PersistentVolume
	PVCs        []corev1.PersistentVolumeClaim
	Services    []corev1.Service
	Endpoints   map[string]*corev1.Endpoints // key: ns/name
	Jobs        []batchv1.Job
	HasMetrics  bool
	PodUsage    map[string]PodUsage // key: ns/name
	Errors      []string            // per-resource collection failures
}

func Key(ns, name string) string { return ns + "/" + name }
