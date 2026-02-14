package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func int32Ptr(i int32) *int32 { return &i }

func TestDiscoverDeployment(t *testing.T) {
	cs := fake.NewSimpleClientset(&appsv1.Deployment{ //nolint:staticcheck // NewClientset requires generated apply configs
		ObjectMeta: metav1.ObjectMeta{Name: "api-gw", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != KindDeployment {
		t.Errorf("kind = %s, want Deployment", w.Kind)
	}
	if w.Name != "api-gw" {
		t.Errorf("name = %s, want api-gw", w.Name)
	}
	if w.Replicas != 3 {
		t.Errorf("replicas = %d, want 3", w.Replicas)
	}
}

func TestDiscoverStatefulSet(t *testing.T) {
	cs := fake.NewSimpleClientset(&appsv1.StatefulSet{ //nolint:staticcheck // NewClientset requires generated apply configs
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: int32Ptr(5)},
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindStatefulSet, "redis")
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != KindStatefulSet {
		t.Errorf("kind = %s, want StatefulSet", w.Kind)
	}
	if w.Replicas != 5 {
		t.Errorf("replicas = %d, want 5", w.Replicas)
	}
}

func TestDiscoverDaemonSet(t *testing.T) {
	cs := fake.NewSimpleClientset(&appsv1.DaemonSet{ //nolint:staticcheck // NewClientset requires generated apply configs
		ObjectMeta: metav1.ObjectMeta{Name: "node-exporter", Namespace: "default"},
		Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 4},
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDaemonSet, "node-exporter")
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != KindDaemonSet {
		t.Errorf("kind = %s, want DaemonSet", w.Kind)
	}
	if w.Replicas != 4 {
		t.Errorf("replicas = %d, want 4", w.Replicas)
	}
}

func TestDiscoverNotFound(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	_, err := DiscoverByName(context.Background(), c, KindDeployment, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent deployment")
	}
}

func TestDiscoverBySelector(t *testing.T) {
	cs := fake.NewSimpleClientset( //nolint:staticcheck // NewClientset requires generated apply configs
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "api-gw",
				Namespace: "default",
				Labels:    map[string]string{"team": "platform"},
			},
			Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "redis",
				Namespace: "default",
				Labels:    map[string]string{"team": "platform"},
			},
			Spec: appsv1.StatefulSetSpec{Replicas: int32Ptr(3)},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other",
				Namespace: "default",
				Labels:    map[string]string{"team": "other"},
			},
			Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
		},
	)
	c := NewClientFromInterface(cs, "default")

	workloads, err := DiscoverBySelector(context.Background(), c, "team=platform")
	if err != nil {
		t.Fatal(err)
	}
	if len(workloads) != 2 {
		t.Errorf("found %d workloads, want 2", len(workloads))
	}
}

func TestDiscoverBySelector_Empty(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	workloads, err := DiscoverBySelector(context.Background(), c, "team=nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(workloads) != 0 {
		t.Errorf("found %d workloads, want 0", len(workloads))
	}
}

func TestDiscoverTapped(t *testing.T) {
	tapped := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api-gw", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
				},
			},
		},
	}
	untapped := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}

	cs := fake.NewSimpleClientset(tapped, untapped) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	workloads, err := DiscoverTapped(context.Background(), c, "logtap.dev/tapped")
	if err != nil {
		t.Fatal(err)
	}
	if len(workloads) != 1 {
		t.Errorf("found %d workloads, want 1", len(workloads))
	}
	if len(workloads) > 0 && workloads[0].Name != "api-gw" {
		t.Errorf("name = %q, want %q", workloads[0].Name, "api-gw")
	}
}

func TestDiscoverTapped_None(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	workloads, err := DiscoverTapped(context.Background(), c, "logtap.dev/tapped")
	if err != nil {
		t.Fatal(err)
	}
	if len(workloads) != 0 {
		t.Errorf("found %d workloads, want 0", len(workloads))
	}
}
