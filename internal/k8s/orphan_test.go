package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func makeOrphanDeployment(name string, annotations map[string]string, containers []corev1.Container) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
				Spec:       corev1.PodSpec{Containers: containers},
			},
		},
	}
}

func TestFindOrphans_OrphanedSidecar(t *testing.T) {
	deploy := makeOrphanDeployment("api-gw",
		map[string]string{
			"logtap.dev/tapped": "lt-a3f9",
			"logtap.dev/target": "logtap:9000",
		},
		[]corev1.Container{
			{Name: "app", Image: "myapp:v1"},
			{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap-forwarder:latest"},
		},
	)
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	unreachable := func(target string) bool { return false }
	result, err := FindOrphans(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-", unreachable)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Sidecars) != 1 {
		t.Fatalf("sidecars = %d, want 1", len(result.Sidecars))
	}
	if result.Sidecars[0].TargetReachable {
		t.Error("TargetReachable = true, want false")
	}
	if result.Sidecars[0].Target != "logtap:9000" {
		t.Errorf("Target = %q, want %q", result.Sidecars[0].Target, "logtap:9000")
	}
	if len(result.StaleWorkloads) != 0 {
		t.Errorf("stale = %d, want 0", len(result.StaleWorkloads))
	}
}

func TestFindOrphans_HealthySidecar(t *testing.T) {
	deploy := makeOrphanDeployment("api-gw",
		map[string]string{
			"logtap.dev/tapped": "lt-a3f9",
			"logtap.dev/target": "logtap:9000",
		},
		[]corev1.Container{
			{Name: "app", Image: "myapp:v1"},
			{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap-forwarder:latest"},
		},
	)
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	reachable := func(target string) bool { return true }
	result, err := FindOrphans(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-", reachable)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Sidecars) != 1 {
		t.Fatalf("sidecars = %d, want 1", len(result.Sidecars))
	}
	if !result.Sidecars[0].TargetReachable {
		t.Error("TargetReachable = false, want true")
	}
}

func TestFindOrphans_StaleAnnotation(t *testing.T) {
	deploy := makeOrphanDeployment("api-gw",
		map[string]string{
			"logtap.dev/tapped": "lt-a3f9",
		},
		[]corev1.Container{
			{Name: "app", Image: "myapp:v1"},
		},
	)
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	result, err := FindOrphans(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Sidecars) != 0 {
		t.Errorf("sidecars = %d, want 0", len(result.Sidecars))
	}
	if len(result.StaleWorkloads) != 1 {
		t.Fatalf("stale = %d, want 1", len(result.StaleWorkloads))
	}
	if result.StaleWorkloads[0].Annotation != "lt-a3f9" {
		t.Errorf("annotation = %q, want %q", result.StaleWorkloads[0].Annotation, "lt-a3f9")
	}
}

func TestFindOrphans_ReceiverPod(t *testing.T) {
	receiverPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ReceiverName,
			Namespace: "default",
			Labels: map[string]string{
				LabelManagedBy: ManagedByValue,
				LabelName:      ReceiverName,
			},
			CreationTimestamp: metav1.Now(),
		},
	}
	cs := fake.NewSimpleClientset(receiverPod) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	result, err := FindOrphans(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Receivers) != 1 {
		t.Fatalf("receivers = %d, want 1", len(result.Receivers))
	}
	if result.Receivers[0].PodName != ReceiverName {
		t.Errorf("PodName = %q, want %q", result.Receivers[0].PodName, ReceiverName)
	}
	if result.Receivers[0].Namespace != "default" {
		t.Errorf("Namespace = %q, want %q", result.Receivers[0].Namespace, "default")
	}
}

func TestFindOrphans_DaemonSet(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "log-agent", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"logtap.dev/tapped": "lt-a3f9",
						"logtap.dev/target": "logtap:9000",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "agent", Image: "agent:v1"},
						{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap-forwarder:latest"},
					},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(ds) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	result, err := FindOrphans(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Sidecars) != 1 {
		t.Fatalf("sidecars = %d, want 1", len(result.Sidecars))
	}
	if result.Sidecars[0].Workload.Kind != KindDaemonSet {
		t.Errorf("kind = %s, want DaemonSet", result.Sidecars[0].Workload.Kind)
	}
}

func TestFindOrphans_StatefulSet(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(3),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"logtap.dev/tapped": "lt-a3f9",
						"logtap.dev/target": "logtap:9000",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "redis", Image: "redis:7"},
						{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap-forwarder:latest"},
					},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(sts) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	reachable := func(target string) bool { return true }
	result, err := FindOrphans(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-", reachable)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Sidecars) != 1 {
		t.Fatalf("sidecars = %d, want 1", len(result.Sidecars))
	}
	if result.Sidecars[0].Workload.Kind != KindStatefulSet {
		t.Errorf("kind = %s, want StatefulSet", result.Sidecars[0].Workload.Kind)
	}
	if !result.Sidecars[0].TargetReachable {
		t.Error("TargetReachable = false, want true")
	}
}

func TestFindOrphans_Clean(t *testing.T) {
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

	result, err := FindOrphans(context.Background(), c, "logtap.dev/tapped", "logtap.dev/target", "logtap-forwarder-", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Sidecars) != 0 {
		t.Errorf("sidecars = %d, want 0", len(result.Sidecars))
	}
	if len(result.StaleWorkloads) != 0 {
		t.Errorf("stale = %d, want 0", len(result.StaleWorkloads))
	}
}
