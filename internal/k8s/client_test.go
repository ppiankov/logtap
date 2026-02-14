package k8s

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestGetClusterInfo(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	info, err := GetClusterInfo(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if info.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", info.Namespace, "test-ns")
	}
	// fake clientset returns a version string
	if info.Version == "" {
		t.Error("Version is empty")
	}
}
