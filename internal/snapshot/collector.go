package snapshot

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Collect lists every resource kind independently: one failure is
// recorded in Errors and never aborts the rest (no silent failures,
// no all-or-nothing). mc may be nil (metrics-server absent).
func Collect(ctx context.Context, kube kubernetes.Interface, mc metricsclient.Interface) *Snapshot {
	s := &Snapshot{
		CollectedAt: time.Now(),
		Endpoints:   map[string]*corev1.Endpoints{},
		PodUsage:    map[string]PodUsage{},
	}
	errf := func(format string, a ...any) { s.Errors = append(s.Errors, fmt.Sprintf(format, a...)) }
	opts := metav1.ListOptions{}

	if l, err := kube.CoreV1().Nodes().List(ctx, opts); err != nil {
		errf("nodes: %v", err)
	} else {
		s.Nodes = l.Items
	}
	if l, err := kube.CoreV1().Pods("").List(ctx, opts); err != nil {
		errf("pods: %v", err)
	} else {
		s.Pods = l.Items
	}
	if l, err := kube.CoreV1().PersistentVolumes().List(ctx, opts); err != nil {
		errf("persistentvolumes: %v", err)
	} else {
		s.PVs = l.Items
	}
	if l, err := kube.CoreV1().PersistentVolumeClaims("").List(ctx, opts); err != nil {
		errf("persistentvolumeclaims: %v", err)
	} else {
		s.PVCs = l.Items
	}
	if l, err := kube.CoreV1().Services("").List(ctx, opts); err != nil {
		errf("services: %v", err)
	} else {
		s.Services = l.Items
	}
	if l, err := kube.CoreV1().Endpoints("").List(ctx, opts); err != nil {
		errf("endpoints: %v", err)
	} else {
		for i := range l.Items {
			e := &l.Items[i]
			s.Endpoints[Key(e.Namespace, e.Name)] = e
		}
	}
	if l, err := kube.BatchV1().Jobs("").List(ctx, opts); err != nil {
		errf("jobs: %v", err)
	} else {
		s.Jobs = l.Items
	}

	if mc != nil {
		if l, err := mc.MetricsV1beta1().PodMetricses("").List(ctx, opts); err != nil {
			// A missing metrics API is expected (HasMetrics stays false, report
			// notes it). Any other failure must surface — never silently.
			if !apierrors.IsNotFound(err) {
				errf("podmetrics: %v", err)
			}
		} else {
			s.HasMetrics = true
			for _, pm := range l.Items {
				var u PodUsage
				for _, c := range pm.Containers {
					u.CPUMilli += c.Usage.Cpu().MilliValue()
					u.MemBytes += c.Usage.Memory().Value()
				}
				s.PodUsage[Key(pm.Namespace, pm.Name)] = u
			}
		}
	}
	return s
}
