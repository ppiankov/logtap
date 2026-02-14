package k8s

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestCheckResources_LimitRangeListError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("list", "limitranges", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected limitrange list error")
	})
	c := NewClientFromInterface(cs, "default")

	_, err := CheckResources(context.Background(), c, 1, "16Mi", "25m")
	if err == nil {
		t.Fatal("expected error for limitrange list failure")
	}
}

func TestCheckResources_NodeListError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected node list error")
	})
	c := NewClientFromInterface(cs, "default")

	_, err := CheckResources(context.Background(), c, 1, "16Mi", "25m")
	if err == nil {
		t.Fatal("expected error for node list failure")
	}
}

func TestCheckResources_QuotaListError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("list", "resourcequotas", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected quota list error")
	})
	c := NewClientFromInterface(cs, "default")

	_, err := CheckResources(context.Background(), c, 1, "16Mi", "25m")
	if err == nil {
		t.Fatal("expected error for quota list failure")
	}
}

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

func TestGetQuotaSummary(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "default-quota", Namespace: "default"},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("4Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("8"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("2"),
			},
		},
	}

	cs := fake.NewSimpleClientset(quota) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	summaries, err := GetQuotaSummary(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}
	if summaries[0].Name != "default-quota" {
		t.Errorf("name = %q, want %q", summaries[0].Name, "default-quota")
	}
	if summaries[0].MemHard != "4Gi" {
		t.Errorf("MemHard = %q, want %q", summaries[0].MemHard, "4Gi")
	}
	if summaries[0].MemUsed != "1Gi" {
		t.Errorf("MemUsed = %q, want %q", summaries[0].MemUsed, "1Gi")
	}
}

func TestGetQuotaSummary_NoQuotas(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	summaries, err := GetQuotaSummary(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Errorf("summaries = %d, want 0", len(summaries))
	}
}

func TestIsProdNamespace(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"env=prod", map[string]string{"env": "prod"}, true},
		{"env=production", map[string]string{"env": "production"}, true},
		{"environment=prod", map[string]string{"environment": "prod"}, true},
		{"logtap.dev/prod=true", map[string]string{"logtap.dev/prod": "true"}, true},
		{"env=staging", map[string]string{"env": "staging"}, false},
		{"no labels", map[string]string{}, false},
		{"unrelated", map[string]string{"team": "platform"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ns", Labels: tt.labels},
			}
			cs := fake.NewSimpleClientset(ns) //nolint:staticcheck // NewClientset requires generated apply configs
			c := NewClientFromInterface(cs, "test-ns")

			got, err := IsProdNamespace(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("IsProdNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckResources_CPULimitRange(t *testing.T) {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Name: "strict-cpu", Namespace: "default"},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Max: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("10m"),
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
		if w.Check == "limitrange" && strings.Contains(w.Message, "cpu") {
			found = true
		}
	}
	if !found {
		t.Error("expected cpu limitrange warning")
	}
}

func TestCheckResources_CPUQuota(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-tight", Namespace: "default"},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("500m"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("480m"),
			},
		},
	}

	cs := fake.NewSimpleClientset(quota) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "default")

	warnings, err := CheckResources(context.Background(), c, 2, "16Mi", "25m")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, w := range warnings {
		if w.Check == "quota" && strings.Contains(w.Message, "cpu") {
			found = true
		}
	}
	if !found {
		t.Error("expected cpu quota warning")
	}
}

func TestCheckResources_DiskPressure(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue},
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
		if w.Check == "capacity" && strings.Contains(w.Message, "disk pressure") {
			found = true
		}
	}
	if !found {
		t.Error("expected disk pressure warning")
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
