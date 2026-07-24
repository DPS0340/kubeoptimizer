package snapshot

import (
	"context"
	"fmt"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Collect lists every resource kind independently: one failure is
// recorded in Errors and never aborts the rest (no silent failures,
// no all-or-nothing). mc may be nil (metrics-server absent).
// namespace scopes namespaced resources; "" means all namespaces.
// Cluster-scoped resources (nodes, PVs) are skipped under a namespace
// filter — the report notes this rather than pretending they were seen.
func Collect(ctx context.Context, kube kubernetes.Interface, mc metricsclient.Interface, namespace string) *Snapshot {
	s := &Snapshot{
		CollectedAt:    time.Now(),
		Namespace:      namespace,
		EndpointSlices: map[string][]discoveryv1.EndpointSlice{},
		PodUsage:       map[string]PodUsage{},
	}
	errf := func(format string, a ...any) { s.Errors = append(s.Errors, fmt.Sprintf(format, a...)) }
	opts := metav1.ListOptions{}

	if namespace == "" {
		if l, err := kube.CoreV1().Nodes().List(ctx, opts); err != nil {
			errf("nodes: %v", err)
		} else {
			s.Nodes = l.Items
		}
		if l, err := kube.CoreV1().PersistentVolumes().List(ctx, opts); err != nil {
			errf("persistentvolumes: %v", err)
		} else {
			s.PVs = l.Items
		}
	}
	if l, err := kube.CoreV1().Pods(namespace).List(ctx, opts); err != nil {
		errf("pods: %v", err)
	} else {
		s.Pods = l.Items
	}
	if l, err := kube.CoreV1().PersistentVolumeClaims(namespace).List(ctx, opts); err != nil {
		errf("persistentvolumeclaims: %v", err)
	} else {
		s.PVCs = l.Items
	}
	if l, err := kube.CoreV1().Services(namespace).List(ctx, opts); err != nil {
		errf("services: %v", err)
	} else {
		s.Services = l.Items
	}
	if l, err := kube.DiscoveryV1().EndpointSlices(namespace).List(ctx, opts); err != nil {
		errf("endpointslices: %v", err)
	} else {
		for i := range l.Items {
			es := l.Items[i]
			svc, ok := es.Labels[discoveryv1.LabelServiceName]
			if !ok {
				continue // not service-managed (custom slice) — irrelevant here
			}
			k := Key(es.Namespace, svc)
			s.EndpointSlices[k] = append(s.EndpointSlices[k], es)
		}
	}
	if l, err := kube.BatchV1().Jobs(namespace).List(ctx, opts); err != nil {
		errf("jobs: %v", err)
	} else {
		s.Jobs = l.Items
	}

	if mc != nil {
		if l, err := mc.MetricsV1beta1().PodMetricses(namespace).List(ctx, opts); err != nil {
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
