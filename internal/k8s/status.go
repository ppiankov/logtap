package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodStatus describes sidecar health in a single pod.
type PodStatus struct {
	Name           string
	SidecarRunning bool
}

// TappedStatus describes a tapped workload with pod-level health.
type TappedStatus struct {
	Workload *Workload
	Sessions []string
	Target   string
	Pods     []PodStatus
	Ready    int
	Total    int
}

// GetTappedStatus returns the status of all tapped workloads including pod health.
func GetTappedStatus(ctx context.Context, c *Client, annotationTapped, annotationTarget, containerPrefix string) ([]TappedStatus, error) {
	tapped, err := DiscoverTapped(ctx, c, annotationTapped)
	if err != nil {
		return nil, err
	}

	var statuses []TappedStatus
	for _, w := range tapped {
		sel := getWorkloadSelector(w)
		sessions := splitAnnotation(w.Annotations[annotationTapped])
		target := w.Annotations[annotationTarget]

		ts := TappedStatus{
			Workload: w,
			Sessions: sessions,
			Target:   target,
		}

		if sel != "" {
			pods, err := c.CS.CoreV1().Pods(c.NS).List(ctx, metav1.ListOptions{LabelSelector: sel})
			if err != nil {
				return nil, fmt.Errorf("list pods for %s/%s: %w", w.Kind, w.Name, err)
			}

			for _, pod := range pods.Items {
				running := isSidecarRunning(pod.Status.ContainerStatuses, containerPrefix)
				ts.Pods = append(ts.Pods, PodStatus{
					Name:           pod.Name,
					SidecarRunning: running,
				})
				ts.Total++
				if running {
					ts.Ready++
				}
			}
		}

		statuses = append(statuses, ts)
	}

	return statuses, nil
}

func getWorkloadSelector(w *Workload) string {
	var labels map[string]string
	switch raw := w.Raw.(type) {
	case *appsv1.Deployment:
		if raw.Spec.Selector != nil {
			labels = raw.Spec.Selector.MatchLabels
		}
	case *appsv1.StatefulSet:
		if raw.Spec.Selector != nil {
			labels = raw.Spec.Selector.MatchLabels
		}
	case *appsv1.DaemonSet:
		if raw.Spec.Selector != nil {
			labels = raw.Spec.Selector.MatchLabels
		}
	}
	if len(labels) == 0 {
		return ""
	}

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(labels))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}

func isSidecarRunning(statuses []corev1.ContainerStatus, prefix string) bool {
	for _, cs := range statuses {
		if strings.HasPrefix(cs.Name, prefix) && cs.State.Running != nil {
			return true
		}
	}
	return false
}

func splitAnnotation(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
