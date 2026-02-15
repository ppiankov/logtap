package k8s

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func makeTestDeployment(name string, containers ...corev1.Container) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}

func sidecarContainer(name string) corev1.Container {
	return corev1.Container{
		Name:  name,
		Image: "ghcr.io/ppiankov/logtap-forwarder:latest",
		Env: []corev1.EnvVar{
			{Name: "LOGTAP_TARGET", Value: "logtap:9000"},
			{Name: "LOGTAP_SESSION", Value: "lt-a3f9"},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("16Mi"),
				corev1.ResourceCPU:    resource.MustParse("25m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
				corev1.ResourceCPU:    resource.MustParse("50m"),
			},
		},
	}
}

func TestApplyPatch_AddContainer(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	deploy := makeTestDeployment("api-gw", app)
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container: sidecarContainer("logtap-forwarder-lt-a3f9"),
		Annotations: map[string]string{
			"logtap.dev/tapped": "lt-a3f9",
			"logtap.dev/target": "logtap:9000",
		},
	}

	diff, err := ApplyPatch(context.Background(), c, w, ps, false)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}

	// Verify update was applied
	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Annotations["logtap.dev/tapped"] != "lt-a3f9" {
		t.Error("annotation not set")
	}
}

func TestApplyPatch_MultiSession(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	first := sidecarContainer("logtap-forwarder-lt-a3f9")
	deploy := makeTestDeployment("api-gw", app, first)
	deploy.Spec.Template.Annotations = map[string]string{
		"logtap.dev/tapped": "lt-a3f9",
		"logtap.dev/target": "logtap:9000",
	}

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container: sidecarContainer("logtap-forwarder-lt-b2c1"),
		Annotations: map[string]string{
			"logtap.dev/tapped": "lt-a3f9,lt-b2c1",
			"logtap.dev/target": "logtap:9000",
		},
	}

	_, err = ApplyPatch(context.Background(), c, w, ps, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 3 {
		t.Errorf("containers = %d, want 3", len(updated.Spec.Template.Spec.Containers))
	}
}

func TestApplyPatch_DryRun(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	deploy := makeTestDeployment("api-gw", app)
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container: sidecarContainer("logtap-forwarder-lt-a3f9"),
		Annotations: map[string]string{
			"logtap.dev/tapped": "lt-a3f9",
		},
	}

	diff, err := ApplyPatch(context.Background(), c, w, ps, true)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("dry-run diff is empty")
	}
	if !strings.Contains(diff, "logtap-forwarder") {
		t.Error("diff does not mention sidecar container")
	}

	// Verify no actual update happened
	original, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("containers = %d, want 1 (dry-run should not modify)", len(original.Spec.Template.Spec.Containers))
	}
}

func TestApplyPatch_StatefulSet(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "redis", Image: "redis:7"}},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(sts) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindStatefulSet, "redis")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container:   sidecarContainer("logtap-forwarder-lt-a3f9"),
		Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
	}

	_, err = ApplyPatch(context.Background(), c, w, ps, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := cs.AppsV1().StatefulSets("default").Get(context.Background(), "redis", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2", len(updated.Spec.Template.Spec.Containers))
	}
}

func TestRemovePatch_Deployment(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	sc := sidecarContainer("logtap-forwarder-lt-a3f9")
	deploy := makeTestDeployment("api-gw", app, sc)
	deploy.Spec.Template.Annotations = map[string]string{
		"logtap.dev/tapped": "lt-a3f9",
		"logtap.dev/target": "logtap:9000",
	}

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	rs := RemovePatchSpec{
		ContainerNames:    []string{"logtap-forwarder-lt-a3f9"},
		DeleteAnnotations: []string{"logtap.dev/tapped", "logtap.dev/target"},
	}

	diff, err := RemovePatch(context.Background(), c, w, rs, false)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("containers = %d, want 1", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Spec.Containers[0].Name != "app" {
		t.Errorf("remaining container = %q, want %q", updated.Spec.Template.Spec.Containers[0].Name, "app")
	}
	if _, ok := updated.Spec.Template.Annotations["logtap.dev/tapped"]; ok {
		t.Error("tapped annotation should be deleted")
	}
}

func TestRemovePatch_StatefulSet(t *testing.T) {
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
						sidecarContainer("logtap-forwarder-lt-a3f9"),
					},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(sts) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindStatefulSet, "redis")
	if err != nil {
		t.Fatal(err)
	}

	rs := RemovePatchSpec{
		ContainerNames:    []string{"logtap-forwarder-lt-a3f9"},
		DeleteAnnotations: []string{"logtap.dev/tapped", "logtap.dev/target"},
	}

	_, err = RemovePatch(context.Background(), c, w, rs, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := cs.AppsV1().StatefulSets("default").Get(context.Background(), "redis", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("containers = %d, want 1", len(updated.Spec.Template.Spec.Containers))
	}
}

func TestRemovePatch_DryRun(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	sc := sidecarContainer("logtap-forwarder-lt-a3f9")
	deploy := makeTestDeployment("api-gw", app, sc)
	deploy.Spec.Template.Annotations = map[string]string{
		"logtap.dev/tapped": "lt-a3f9",
	}

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	rs := RemovePatchSpec{
		ContainerNames:    []string{"logtap-forwarder-lt-a3f9"},
		DeleteAnnotations: []string{"logtap.dev/tapped"},
	}

	diff, err := RemovePatch(context.Background(), c, w, rs, true)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("dry-run diff is empty")
	}

	// Verify no changes applied
	original, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2 (dry-run should not modify)", len(original.Spec.Template.Spec.Containers))
	}
}

func TestApplyPatch_UnsupportedKind(t *testing.T) {
	w := &Workload{Kind: WorkloadKind("CronJob"), Name: "test"}
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	_, err := ApplyPatch(context.Background(), c, w, PatchSpec{}, false)
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("err = %q, want 'unsupported'", err.Error())
	}
}

func TestRemovePatch_UnsupportedKind(t *testing.T) {
	w := &Workload{Kind: WorkloadKind("CronJob"), Name: "test"}
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	_, err := RemovePatch(context.Background(), c, w, RemovePatchSpec{}, false)
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("err = %q, want 'unsupported'", err.Error())
	}
}

func TestApplyPatch_DaemonSet(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "log-agent", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "agent", Image: "agent:v1"}},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(ds) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDaemonSet, "log-agent")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container:   sidecarContainer("logtap-forwarder-lt-a3f9"),
		Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
	}

	diff, err := ApplyPatch(context.Background(), c, w, ps, false)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}

	updated, err := cs.AppsV1().DaemonSets("default").Get(context.Background(), "log-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Annotations["logtap.dev/tapped"] != "lt-a3f9" {
		t.Error("annotation not set")
	}
}

func TestRemovePatch_DaemonSet(t *testing.T) {
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
						sidecarContainer("logtap-forwarder-lt-a3f9"),
					},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(ds) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDaemonSet, "log-agent")
	if err != nil {
		t.Fatal(err)
	}

	rs := RemovePatchSpec{
		ContainerNames:    []string{"logtap-forwarder-lt-a3f9"},
		DeleteAnnotations: []string{"logtap.dev/tapped", "logtap.dev/target"},
	}

	diff, err := RemovePatch(context.Background(), c, w, rs, false)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Error("diff is empty")
	}

	updated, err := cs.AppsV1().DaemonSets("default").Get(context.Background(), "log-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 1 {
		t.Errorf("containers = %d, want 1", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Spec.Containers[0].Name != "agent" {
		t.Errorf("remaining = %q, want %q", updated.Spec.Template.Spec.Containers[0].Name, "agent")
	}
}

func TestApplyPatch_DaemonSet_UpdateError(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "log-agent", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "agent", Image: "agent:v1"}},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(ds) //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("update", "daemonsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected update error")
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDaemonSet, "log-agent")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container:   sidecarContainer("logtap-forwarder-lt-a3f9"),
		Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
	}

	_, err = ApplyPatch(context.Background(), c, w, ps, false)
	if err == nil {
		t.Fatal("expected error for update failure")
	}
	if !strings.Contains(err.Error(), "update daemonset") {
		t.Errorf("err = %q, want 'update daemonset'", err.Error())
	}
}

func TestRemovePatch_MultiSession_KeepOne(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	sc1 := sidecarContainer("logtap-forwarder-lt-a3f9")
	sc2 := sidecarContainer("logtap-forwarder-lt-b2c1")
	deploy := makeTestDeployment("api-gw", app, sc1, sc2)
	deploy.Spec.Template.Annotations = map[string]string{
		"logtap.dev/tapped": "lt-a3f9,lt-b2c1",
		"logtap.dev/target": "logtap:9000",
	}

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	// Remove only lt-a3f9, keep lt-b2c1
	rs := RemovePatchSpec{
		ContainerNames: []string{"logtap-forwarder-lt-a3f9"},
		SetAnnotations: map[string]string{"logtap.dev/tapped": "lt-b2c1"},
	}

	_, err = RemovePatch(context.Background(), c, w, rs, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 2 {
		t.Errorf("containers = %d, want 2 (app + lt-b2c1)", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Annotations["logtap.dev/tapped"] != "lt-b2c1" {
		t.Errorf("tapped = %q, want %q", updated.Spec.Template.Annotations["logtap.dev/tapped"], "lt-b2c1")
	}
	if updated.Spec.Template.Annotations["logtap.dev/target"] != "logtap:9000" {
		t.Error("target annotation should be preserved")
	}
}

func TestRemovePatch_DaemonSet_UpdateError(t *testing.T) {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "log-agent", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "agent", Image: "agent:v1"},
						{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap:latest"},
					},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(ds) //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("update", "daemonsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected update error")
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDaemonSet, "log-agent")
	if err != nil {
		t.Fatal(err)
	}

	rs := RemovePatchSpec{
		ContainerNames:    []string{"logtap-forwarder-lt-a3f9"},
		DeleteAnnotations: []string{"logtap.dev/tapped"},
	}

	_, err = RemovePatch(context.Background(), c, w, rs, false)
	if err == nil {
		t.Fatal("expected error for update failure")
	}
	if !strings.Contains(err.Error(), "update daemonset") {
		t.Errorf("err = %q, want 'update daemonset'", err.Error())
	}
}

func TestApplyPatch_WithVolumes(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	deploy := makeTestDeployment("api-gw", app)
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container: sidecarContainer("logtap-forwarder-lt-a3f9"),
		Volumes: []corev1.Volume{
			{Name: "config-vol", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			{Name: "logs-vol", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		},
		Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
	}

	_, err = ApplyPatch(context.Background(), c, w, ps, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Volumes) != 2 {
		t.Errorf("volumes = %d, want 2", len(updated.Spec.Template.Spec.Volumes))
	}
}

func TestRemovePatch_WithVolumes(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	sc := sidecarContainer("logtap-forwarder-lt-a3f9")
	deploy := makeTestDeployment("api-gw", app, sc)
	deploy.Spec.Template.Annotations = map[string]string{
		"logtap.dev/tapped": "lt-a3f9",
	}
	deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
		{Name: "config-vol", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "logs-vol", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "app-data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	rs := RemovePatchSpec{
		ContainerNames:    []string{"logtap-forwarder-lt-a3f9"},
		VolumeNames:       []string{"config-vol", "logs-vol"},
		DeleteAnnotations: []string{"logtap.dev/tapped"},
	}

	_, err = RemovePatch(context.Background(), c, w, rs, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := cs.AppsV1().Deployments("default").Get(context.Background(), "api-gw", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Template.Spec.Volumes) != 1 {
		t.Errorf("volumes = %d, want 1", len(updated.Spec.Template.Spec.Volumes))
	}
	if updated.Spec.Template.Spec.Volumes[0].Name != "app-data" {
		t.Errorf("remaining volume = %q, want %q", updated.Spec.Template.Spec.Volumes[0].Name, "app-data")
	}
}

func TestApplyPatch_Deployment_UpdateError(t *testing.T) {
	app := corev1.Container{Name: "app", Image: "myapp:v1"}
	deploy := makeTestDeployment("api-gw", app)
	cs := fake.NewSimpleClientset(deploy) //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected update error")
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindDeployment, "api-gw")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container:   sidecarContainer("logtap-forwarder-lt-a3f9"),
		Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
	}

	_, err = ApplyPatch(context.Background(), c, w, ps, false)
	if err == nil {
		t.Fatal("expected error for update failure")
	}
	if !strings.Contains(err.Error(), "update deployment") {
		t.Errorf("err = %q, want 'update deployment'", err.Error())
	}
}

func TestApplyPatch_StatefulSet_UpdateError(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "redis", Image: "redis:7"}},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(sts) //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("update", "statefulsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected update error")
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindStatefulSet, "redis")
	if err != nil {
		t.Fatal(err)
	}

	ps := PatchSpec{
		Container:   sidecarContainer("logtap-forwarder-lt-a3f9"),
		Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
	}

	_, err = ApplyPatch(context.Background(), c, w, ps, false)
	if err == nil {
		t.Fatal("expected error for update failure")
	}
	if !strings.Contains(err.Error(), "update statefulset") {
		t.Errorf("err = %q, want 'update statefulset'", err.Error())
	}
}

func TestRemovePatch_StatefulSet_UpdateError(t *testing.T) {
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"logtap.dev/tapped": "lt-a3f9"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "postgres", Image: "postgres:15"},
						{Name: "logtap-forwarder-lt-a3f9", Image: "ghcr.io/ppiankov/logtap:latest"},
					},
				},
			},
		},
	}
	cs := fake.NewSimpleClientset(ss) //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("update", "statefulsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected update error")
	})
	c := NewClientFromInterface(cs, "default")

	w, err := DiscoverByName(context.Background(), c, KindStatefulSet, "db")
	if err != nil {
		t.Fatal(err)
	}

	rs := RemovePatchSpec{
		ContainerNames:    []string{"logtap-forwarder-lt-a3f9"},
		DeleteAnnotations: []string{"logtap.dev/tapped"},
	}

	_, err = RemovePatch(context.Background(), c, w, rs, false)
	if err == nil {
		t.Fatal("expected error for update failure")
	}
	if !strings.Contains(err.Error(), "update statefulset") {
		t.Errorf("err = %q, want 'update statefulset'", err.Error())
	}
}
