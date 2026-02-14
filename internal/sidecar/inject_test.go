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

func int32Ptr(i int32) *int32 { return &i }

func makeDeployment(name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "myapp:v1"},
					},
				},
			},
		},
	}
}

func TestInject_HappyPath(t *testing.T) {
	deploy := makeDeployment("api-gw")
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	cfg := SidecarConfig{
		SessionID: "lt-a3f9",
		Target:    "logtap:9000",
	}

	result, err := Inject(context.Background(), c, w, cfg, false)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Applied {
		t.Error("Applied = false, want true")
	}
	if result.SessionID != "lt-a3f9" {
		t.Errorf("SessionID = %q, want %q", result.SessionID, "lt-a3f9")
	}
	if result.Diff == "" {
		t.Error("Diff is empty")
	}

	// Verify the deployment was updated
	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Annotations[AnnotationTapped] != "lt-a3f9" {
		t.Errorf("tapped annotation = %q, want %q", updated.Spec.Template.Annotations[AnnotationTapped], "lt-a3f9")
	}
}

func TestInject_MultiSession(t *testing.T) {
	deploy := makeDeployment("api-gw")
	deploy.Spec.Template.Annotations = map[string]string{
		AnnotationTapped: "lt-a3f9",
		AnnotationTarget: "logtap:9000",
	}
	deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers,
		corev1.Container{Name: "logtap-forwarder-lt-a3f9", Image: DefaultImage})

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	cfg := SidecarConfig{
		SessionID: "lt-b2c1",
		Target:    "logtap:9000",
	}

	result, err := Inject(context.Background(), c, w, cfg, false)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Applied {
		t.Error("Applied = false, want true")
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 3 {
		t.Errorf("containers = %d, want 3", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Annotations[AnnotationTapped] != "lt-a3f9,lt-b2c1" {
		t.Errorf("tapped = %q, want %q", updated.Spec.Template.Annotations[AnnotationTapped], "lt-a3f9,lt-b2c1")
	}
}

func TestInject_AlreadyTapped(t *testing.T) {
	deploy := makeDeployment("api-gw")
	deploy.Spec.Template.Annotations = map[string]string{
		AnnotationTapped: "lt-a3f9",
		AnnotationTarget: "logtap:9000",
	}

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	cfg := SidecarConfig{
		SessionID: "lt-a3f9",
		Target:    "logtap:9000",
	}

	_, err = Inject(context.Background(), c, w, cfg, false)
	if err == nil {
		t.Error("expected error for already-tapped workload")
	}
}

func TestInject_DryRun(t *testing.T) {
	deploy := makeDeployment("api-gw")
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := k8s.NewClientFromInterface(cs, "default")

	w, err := k8s.DiscoverByName(context.Background(), c, k8s.KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	cfg := SidecarConfig{
		SessionID: "lt-a3f9",
		Target:    "logtap:9000",
	}

	result, err := Inject(context.Background(), c, w, cfg, true)
	if err != nil {
		t.Fatal(err)
	}

	if result.Applied {
		t.Error("Applied = true, want false for dry-run")
	}
	if result.Diff == "" {
		t.Error("dry-run diff is empty")
	}

	// Verify no update
	original, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("containers = %d, want 1 (should not be modified)", len(original.Spec.Template.Spec.Containers))
	}
}
