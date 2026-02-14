package sidecar

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/ppiankov/logtap/internal/k8s"
)

func makeTappedDeployment(name string, sessions ...string) *appsv1.Deployment {
	containers := []corev1.Container{{Name: "app", Image: "myapp:v1"}}
	for _, s := range sessions {
		containers = append(containers, corev1.Container{
			Name:  ContainerPrefix + s,
			Image: DefaultImage,
		})
	}

	tapped := ""
	for i, s := range sessions {
		if i > 0 {
			tapped += ","
		}
		tapped += s
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationTapped: tapped,
						AnnotationTarget: "logtap:9000",
					},
				},
				Spec: corev1.PodSpec{Containers: containers},
			},
		},
	}
}

func TestRemove_SingleSession(t *testing.T) {
	deploy := makeTappedDeployment("api-gw", "lt-a3f9")
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	result, err := Remove(context.Background(), c, w, "lt-a3f9", false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Error("Applied = false, want true")
	}
	if result.Diff == "" {
		t.Error("Diff is empty")
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("containers = %d, want 1", len(updated.Spec.Template.Spec.Containers))
	}
	if _, ok := updated.Spec.Template.Annotations[AnnotationTapped]; ok {
		t.Error("tapped annotation should be deleted")
	}
	if _, ok := updated.Spec.Template.Annotations[AnnotationTarget]; ok {
		t.Error("target annotation should be deleted")
	}
}

func TestRemove_MultiSession(t *testing.T) {
	deploy := makeTappedDeployment("api-gw", "lt-a3f9", "lt-b2c1")
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	result, err := Remove(context.Background(), c, w, "lt-a3f9", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "lt-a3f9" {
		t.Errorf("SessionID = %q, want %q", result.SessionID, "lt-a3f9")
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// app + lt-b2c1 remain
	if len(updated.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Annotations[AnnotationTapped] != "lt-b2c1" {
		t.Errorf("tapped = %q, want %q", updated.Spec.Template.Annotations[AnnotationTapped], "lt-b2c1")
	}
	if updated.Spec.Template.Annotations[AnnotationTarget] != "logtap:9000" {
		t.Error("target annotation should be preserved")
	}
}

func TestRemove_NotTapped(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api-gw", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "myapp:v1"}},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	_, err = Remove(context.Background(), c, w, "lt-a3f9", false)
	if err == nil {
		t.Error("expected error for untapped workload")
	}
}

func TestRemove_SessionNotFound(t *testing.T) {
	deploy := makeTappedDeployment("api-gw", "lt-a3f9")
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	_, err = Remove(context.Background(), c, w, "lt-b2c1", false)
	if err == nil {
		t.Error("expected error for session not found")
	}
}

func TestRemove_DryRun(t *testing.T) {
	deploy := makeTappedDeployment("api-gw", "lt-a3f9")
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	result, err := Remove(context.Background(), c, w, "lt-a3f9", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied {
		t.Error("Applied = true, want false for dry-run")
	}
	if result.Diff == "" {
		t.Error("dry-run diff is empty")
	}

	// Verify no changes
	original, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2 (should not be modified)", len(original.Spec.Template.Spec.Containers))
	}
}

func TestRemoveAll(t *testing.T) {
	deploy := makeTappedDeployment("api-gw", "lt-a3f9", "lt-b2c1")
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	results, err := RemoveAll(context.Background(), c, w, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("results = %d, want 2", len(results))
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("containers = %d, want 1 (only app)", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Spec.Containers[0].Name != "app" {
		t.Errorf("remaining = %q, want %q", updated.Spec.Template.Spec.Containers[0].Name, "app")
	}
	if _, ok := updated.Spec.Template.Annotations[AnnotationTapped]; ok {
		t.Error("tapped annotation should be deleted")
	}
	if _, ok := updated.Spec.Template.Annotations[AnnotationTarget]; ok {
		t.Error("target annotation should be deleted")
	}
}

func TestRemoveAll_NotTapped(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api-gw", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "myapp:v1"}},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	_, err = RemoveAll(context.Background(), c, w, false)
	if err == nil {
		t.Error("expected error for untapped workload")
	}
}
