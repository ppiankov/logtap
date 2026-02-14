package k8s

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func tappedDeploymentWithSelector(name string, labels map[string]string, annotations map[string]string, containers []corev1.Container) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{Containers: containers},
			},
		},
	}
}

func makePod(name string, labels map[string]string, containerStatuses []corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
		Status:     corev1.PodStatus{ContainerStatuses: containerStatuses},
	}
}

func TestGetTappedStatus_Running(t *testing.T) {
	labels := map[string]string{"app": "api-gw"}
	deploy := tappedDeploymentWithSelector("api-gw", labels,
		map[string]string{
			"logtap.dev/tapped": "lt-a3f9",
			"logtap.dev/target": "logtap:9000",
		},
		[]corev1.Container{
			{Name: "app", Image: "myapp:v1"},
			{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap-forwarder:latest"},
		},
	)

	now := metav1.NewTime(time.Now())
	pod1 := makePod("api-gw-abc", labels, []corev1.ContainerStatus{
		{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}},
		{Name: "logtap-forwarder-lt-a3f9", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}},
	})
	pod2 := makePod("api-gw-def", labels, []corev1.ContainerStatus{
		{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}},
		{Name: "logtap-forwarder-lt-a3f9", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}},
	})

	cs := fake.NewSimpleClientset(deploy, pod1, pod2) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	statuses, err := GetTappedStatus(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	if statuses[0].Ready != 2 {
		t.Errorf("Ready = %d, want 2", statuses[0].Ready)
	}
	if statuses[0].Total != 2 {
		t.Errorf("Total = %d, want 2", statuses[0].Total)
	}
	if statuses[0].Target != "logtap:9000" {
		t.Errorf("Target = %q, want %q", statuses[0].Target, "logtap:9000")
	}
	if len(statuses[0].Sessions) != 1 || statuses[0].Sessions[0] != "lt-a3f9" {
		t.Errorf("Sessions = %v, want [lt-a3f9]", statuses[0].Sessions)
	}
}

func TestGetTappedStatus_Partial(t *testing.T) {
	labels := map[string]string{"app": "api-gw"}
	deploy := tappedDeploymentWithSelector("api-gw", labels,
		map[string]string{
			"logtap.dev/tapped": "lt-a3f9",
			"logtap.dev/target": "logtap:9000",
		},
		[]corev1.Container{
			{Name: "app", Image: "myapp:v1"},
			{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap-forwarder:latest"},
		},
	)

	now := metav1.NewTime(time.Now())
	// Pod 1: sidecar running
	pod1 := makePod("api-gw-abc", labels, []corev1.ContainerStatus{
		{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}},
		{Name: "logtap-forwarder-lt-a3f9", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}},
	})
	// Pod 2: sidecar not running (waiting)
	pod2 := makePod("api-gw-def", labels, []corev1.ContainerStatus{
		{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}}},
		{Name: "logtap-forwarder-lt-a3f9", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
	})

	cs := fake.NewSimpleClientset(deploy, pod1, pod2) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	statuses, err := GetTappedStatus(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	if statuses[0].Ready != 1 {
		t.Errorf("Ready = %d, want 1", statuses[0].Ready)
	}
	if statuses[0].Total != 2 {
		t.Errorf("Total = %d, want 2", statuses[0].Total)
	}
}

func TestGetTappedStatus_NoTapped(t *testing.T) {
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
	c := NewClientFromInterface(cs, "default")

	statuses, err := GetTappedStatus(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Errorf("statuses = %d, want 0", len(statuses))
	}
}
