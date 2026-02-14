package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadKind identifies the type of Kubernetes workload.
type WorkloadKind string

const (
	KindDeployment  WorkloadKind = "Deployment"
	KindStatefulSet WorkloadKind = "StatefulSet"
	KindDaemonSet   WorkloadKind = "DaemonSet"
)

// Workload is a normalized representation of a Kubernetes workload.
type Workload struct {
	Kind        WorkloadKind
	Name        string
	Namespace   string
	Replicas    int32
	Annotations map[string]string
	Raw         any
}

func workloadFromDeployment(d *appsv1.Deployment) *Workload {
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	ann := d.Spec.Template.Annotations
	if ann == nil {
		ann = make(map[string]string)
	}
	return &Workload{
		Kind:        KindDeployment,
		Name:        d.Name,
		Namespace:   d.Namespace,
		Replicas:    replicas,
		Annotations: ann,
		Raw:         d,
	}
}

func workloadFromStatefulSet(s *appsv1.StatefulSet) *Workload {
	replicas := int32(1)
	if s.Spec.Replicas != nil {
		replicas = *s.Spec.Replicas
	}
	ann := s.Spec.Template.Annotations
	if ann == nil {
		ann = make(map[string]string)
	}
	return &Workload{
		Kind:        KindStatefulSet,
		Name:        s.Name,
		Namespace:   s.Namespace,
		Replicas:    replicas,
		Annotations: ann,
		Raw:         s,
	}
}

func workloadFromDaemonSet(d *appsv1.DaemonSet) *Workload {
	ann := d.Spec.Template.Annotations
	if ann == nil {
		ann = make(map[string]string)
	}
	return &Workload{
		Kind:        KindDaemonSet,
		Name:        d.Name,
		Namespace:   d.Namespace,
		Replicas:    d.Status.DesiredNumberScheduled,
		Annotations: ann,
		Raw:         d,
	}
}

// DiscoverByName finds a single workload by kind and name.
func DiscoverByName(ctx context.Context, c *Client, kind WorkloadKind, name string) (*Workload, error) {
	switch kind {
	case KindDeployment:
		d, err := c.CS.AppsV1().Deployments(c.NS).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get deployment %s: %w", name, err)
		}
		return workloadFromDeployment(d), nil
	case KindStatefulSet:
		s, err := c.CS.AppsV1().StatefulSets(c.NS).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get statefulset %s: %w", name, err)
		}
		return workloadFromStatefulSet(s), nil
	case KindDaemonSet:
		d, err := c.CS.AppsV1().DaemonSets(c.NS).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get daemonset %s: %w", name, err)
		}
		return workloadFromDaemonSet(d), nil
	default:
		return nil, fmt.Errorf("unsupported workload kind: %s", kind)
	}
}

// DiscoverBySelector finds all workloads matching a label selector.
func DiscoverBySelector(ctx context.Context, c *Client, selector string) ([]*Workload, error) {
	opts := metav1.ListOptions{LabelSelector: selector}
	var workloads []*Workload

	deps, err := c.CS.AppsV1().Deployments(c.NS).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	for i := range deps.Items {
		workloads = append(workloads, workloadFromDeployment(&deps.Items[i]))
	}

	sts, err := c.CS.AppsV1().StatefulSets(c.NS).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	for i := range sts.Items {
		workloads = append(workloads, workloadFromStatefulSet(&sts.Items[i]))
	}

	dss, err := c.CS.AppsV1().DaemonSets(c.NS).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list daemonsets: %w", err)
	}
	for i := range dss.Items {
		workloads = append(workloads, workloadFromDaemonSet(&dss.Items[i]))
	}

	return workloads, nil
}

// DiscoverTapped finds all workloads with a non-empty template annotation for the given key.
func DiscoverTapped(ctx context.Context, c *Client, annotationKey string) ([]*Workload, error) {
	all, err := DiscoverBySelector(ctx, c, "")
	if err != nil {
		return nil, err
	}
	var tapped []*Workload
	for _, w := range all {
		if w.Annotations[annotationKey] != "" {
			tapped = append(tapped, w)
		}
	}
	return tapped, nil
}
