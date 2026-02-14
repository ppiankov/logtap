package k8s

import (
	"context"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// OrphanedSidecar describes a workload with logtap sidecar containers still present.
type OrphanedSidecar struct {
	Workload        *Workload
	Sessions        []string
	Target          string
	TargetReachable bool
}

// StaleAnnotation describes a workload with logtap annotation but no sidecar container.
type StaleAnnotation struct {
	Workload   *Workload
	Annotation string
}

// OrphanResult contains all detected orphaned artifacts.
type OrphanResult struct {
	Sidecars       []OrphanedSidecar
	StaleWorkloads []StaleAnnotation
}

// ReceiverChecker tests if a receiver target is reachable.
type ReceiverChecker func(target string) bool

// FindOrphans detects leftover logtap artifacts in the namespace.
func FindOrphans(ctx context.Context, c *Client, annotationTapped, annotationTarget, containerPrefix string, checkReceiver ReceiverChecker) (*OrphanResult, error) {
	tapped, err := DiscoverTapped(ctx, c, annotationTapped)
	if err != nil {
		return nil, err
	}

	result := &OrphanResult{}
	for _, w := range tapped {
		containers := getTemplateContainers(w)
		hasSidecar := false
		for _, ct := range containers {
			if strings.HasPrefix(ct.Name, containerPrefix) {
				hasSidecar = true
				break
			}
		}

		if hasSidecar {
			target := w.Annotations[annotationTarget]
			reachable := false
			if target != "" && checkReceiver != nil {
				reachable = checkReceiver(target)
			}

			sessions := splitSessions(w.Annotations[annotationTapped])
			result.Sidecars = append(result.Sidecars, OrphanedSidecar{
				Workload:        w,
				Sessions:        sessions,
				Target:          target,
				TargetReachable: reachable,
			})
		} else {
			result.StaleWorkloads = append(result.StaleWorkloads, StaleAnnotation{
				Workload:   w,
				Annotation: w.Annotations[annotationTapped],
			})
		}
	}

	return result, nil
}

func getTemplateContainers(w *Workload) []corev1.Container {
	switch raw := w.Raw.(type) {
	case *appsv1.Deployment:
		return raw.Spec.Template.Spec.Containers
	case *appsv1.StatefulSet:
		return raw.Spec.Template.Spec.Containers
	case *appsv1.DaemonSet:
		return raw.Spec.Template.Spec.Containers
	default:
		return nil
	}
}

func splitSessions(annotation string) []string {
	if annotation == "" {
		return nil
	}
	parts := strings.Split(annotation, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
