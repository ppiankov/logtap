package k8s

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestCheckRBAC_AllAllowed(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		sar := action.(k8stesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		sar.Status.Allowed = true
		return true, sar, nil
	})
	c := NewClientFromInterface(cs, "default")

	checks := []RBACCheck{
		{Resource: "deployments", Verb: "get", Group: "apps"},
		{Resource: "deployments", Verb: "patch", Group: "apps"},
		{Resource: "pods", Verb: "create", Group: ""},
	}

	results, err := CheckRBAC(context.Background(), c, checks)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	for _, r := range results {
		if !r.Allowed {
			t.Errorf("%s %s/%s: allowed = false, want true", r.Check.Verb, r.Check.Group, r.Check.Resource)
		}
	}
}

func TestCheckRBAC_SomeDenied(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		sar := action.(k8stesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		attrs := sar.Spec.ResourceAttributes
		// Deny "patch" on deployments
		if attrs.Resource == "deployments" && attrs.Verb == "patch" {
			sar.Status.Allowed = false
		} else {
			sar.Status.Allowed = true
		}
		return true, sar, nil
	})
	c := NewClientFromInterface(cs, "default")

	checks := []RBACCheck{
		{Resource: "deployments", Verb: "get", Group: "apps"},
		{Resource: "deployments", Verb: "patch", Group: "apps"},
		{Resource: "pods", Verb: "create", Group: ""},
	}

	results, err := CheckRBAC(context.Background(), c, checks)
	if err != nil {
		t.Fatal(err)
	}

	deniedCount := 0
	for _, r := range results {
		if !r.Allowed {
			deniedCount++
			if r.Check.Resource != "deployments" || r.Check.Verb != "patch" {
				t.Errorf("unexpected denial: %s %s/%s", r.Check.Verb, r.Check.Group, r.Check.Resource)
			}
		}
	}
	if deniedCount != 1 {
		t.Errorf("denied = %d, want 1", deniedCount)
	}
}
