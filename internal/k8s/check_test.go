package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckResources_TightQuota(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "tight", Namespace: "default"},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("100Mi"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("90Mi"),
			},
		},
	}

	cs := fake.NewSimpleClientset(quota) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	warnings, err := CheckResources(context.Background(), c, 3, "16Mi", "25m")
	if err != nil {
		t.Fatal(err)
	}

	if len(warnings) == 0 {
		t.Error("expected quota warning for tight memory")
	}
	found := false
	for _, w := range warnings {
		if w.Check == "quota" {
			found = true
		}
	}
	if !found {
		t.Error("expected a quota warning")
	}
}

func TestCheckResources_Headroom(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "relaxed", Namespace: "default"},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("100Mi"),
			},
		},
	}

	cs := fake.NewSimpleClientset(quota) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	warnings, err := CheckResources(context.Background(), c, 2, "16Mi", "25m")
	if err != nil {
		t.Fatal(err)
	}

	for _, w := range warnings {
		if w.Check == "quota" {
			t.Errorf("unexpected quota warning: %s", w.Message)
		}
	}
}

func TestCheckResources_LimitRangeConflict(t *testing.T) {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Name: "strict", Namespace: "default"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Max: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("8Mi"),
					},
				},
			},
		},
	}

	cs := fake.NewSimpleClientset(lr) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	warnings, err := CheckResources(context.Background(), c, 1, "16Mi", "25m")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, w := range warnings {
		if w.Check == "limitrange" {
			found = true
		}
	}
	if !found {
		t.Error("expected limitrange warning")
	}
}

func TestCheckResources_NoQuotas(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	warnings, err := CheckResources(context.Background(), c, 2, "16Mi", "25m")
	if err != nil {
		t.Fatal(err)
	}

	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
}

func TestCheckResources_MemoryPressure(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
	}

	cs := fake.NewSimpleClientset(node) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	warnings, err := CheckResources(context.Background(), c, 1, "16Mi", "25m")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, w := range warnings {
		if w.Check == "capacity" {
			found = true
		}
	}
	if !found {
		t.Error("expected capacity warning for memory pressure")
	}
}
