package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// PatchSpec describes the sidecar container and annotations to add.
type PatchSpec struct {
	Container   corev1.Container
	Annotations map[string]string
}

// ApplyPatch adds a sidecar container and annotations to a workload.
// If dryRun is true, the diff is computed but the workload is not modified.
func ApplyPatch(ctx context.Context, c *Client, w *Workload, ps PatchSpec, dryRun bool) (string, error) {
	switch w.Kind {
	case KindDeployment:
		return applyDeploymentPatch(ctx, c, w.Raw.(*appsv1.Deployment), ps, dryRun)
	case KindStatefulSet:
		return applyStatefulSetPatch(ctx, c, w.Raw.(*appsv1.StatefulSet), ps, dryRun)
	case KindDaemonSet:
		return applyDaemonSetPatch(ctx, c, w.Raw.(*appsv1.DaemonSet), ps, dryRun)
	default:
		return "", fmt.Errorf("unsupported workload kind: %s", w.Kind)
	}
}

func applyDeploymentPatch(ctx context.Context, c *Client, d *appsv1.Deployment, ps PatchSpec, dryRun bool) (string, error) {
	before, _ := marshalYAMLSpec(d)

	updated := d.DeepCopy()
	updated.Spec.Template.Spec.Containers = append(updated.Spec.Template.Spec.Containers, ps.Container)
	if updated.Spec.Template.Annotations == nil {
		updated.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range ps.Annotations {
		updated.Spec.Template.Annotations[k] = v
	}

	after, _ := marshalYAMLSpec(updated)
	diff := computeDiff(before, after)

	if dryRun {
		return diff, nil
	}

	_, err := c.CS.AppsV1().Deployments(c.NS).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("update deployment %s: %w", d.Name, err)
	}
	return diff, nil
}

func applyStatefulSetPatch(ctx context.Context, c *Client, s *appsv1.StatefulSet, ps PatchSpec, dryRun bool) (string, error) {
	before, _ := marshalYAMLSpec(s)

	updated := s.DeepCopy()
	updated.Spec.Template.Spec.Containers = append(updated.Spec.Template.Spec.Containers, ps.Container)
	if updated.Spec.Template.Annotations == nil {
		updated.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range ps.Annotations {
		updated.Spec.Template.Annotations[k] = v
	}

	after, _ := marshalYAMLSpec(updated)
	diff := computeDiff(before, after)

	if dryRun {
		return diff, nil
	}

	_, err := c.CS.AppsV1().StatefulSets(c.NS).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("update statefulset %s: %w", s.Name, err)
	}
	return diff, nil
}

func applyDaemonSetPatch(ctx context.Context, c *Client, d *appsv1.DaemonSet, ps PatchSpec, dryRun bool) (string, error) {
	before, _ := marshalYAMLSpec(d)

	updated := d.DeepCopy()
	updated.Spec.Template.Spec.Containers = append(updated.Spec.Template.Spec.Containers, ps.Container)
	if updated.Spec.Template.Annotations == nil {
		updated.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range ps.Annotations {
		updated.Spec.Template.Annotations[k] = v
	}

	after, _ := marshalYAMLSpec(updated)
	diff := computeDiff(before, after)

	if dryRun {
		return diff, nil
	}

	_, err := c.CS.AppsV1().DaemonSets(c.NS).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("update daemonset %s: %w", d.Name, err)
	}
	return diff, nil
}

func marshalYAMLSpec(obj any) (string, error) {
	j, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	y, err := yaml.JSONToYAML(j)
	if err != nil {
		return "", err
	}
	return string(y), nil
}

func computeDiff(before, after string) string {
	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")

	// Build set of before lines for simple diff
	beforeSet := make(map[string]bool, len(beforeLines))
	for _, l := range beforeLines {
		beforeSet[l] = true
	}
	afterSet := make(map[string]bool, len(afterLines))
	for _, l := range afterLines {
		afterSet[l] = true
	}

	var sb strings.Builder
	for _, l := range beforeLines {
		if !afterSet[l] {
			sb.WriteString("- ")
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}
	for _, l := range afterLines {
		if !beforeSet[l] {
			sb.WriteString("+ ")
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
