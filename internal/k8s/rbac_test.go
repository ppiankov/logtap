package k8s

import (
	"context"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestEnsureForwarderRBAC_Creates(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck
	c := NewClientFromInterface(cs, "test-ns")

	err := EnsureForwarderRBAC(context.Background(), c, []string{"default", "my-sa"}, false)
	if err != nil {
		t.Fatal(err)
	}

	role, err := cs.RbacV1().Roles("test-ns").Get(context.Background(), "logtap-forwarder", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("role not created: %v", err)
	}
	if len(role.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(role.Rules))
	}
	if role.Rules[0].Resources[0] != "pods" || role.Rules[0].Resources[1] != "pods/log" {
		t.Errorf("resources = %v, want [pods pods/log]", role.Rules[0].Resources)
	}

	rb, err := cs.RbacV1().RoleBindings("test-ns").Get(context.Background(), "logtap-forwarder", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("rolebinding not created: %v", err)
	}
	if len(rb.Subjects) != 2 {
		t.Fatalf("subjects = %d, want 2", len(rb.Subjects))
	}
	if rb.RoleRef.Name != "logtap-forwarder" || rb.RoleRef.Kind != "Role" {
		t.Errorf("roleref = %+v, want Role/logtap-forwarder", rb.RoleRef)
	}
}

func TestEnsureForwarderRBAC_Updates(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck
	c := NewClientFromInterface(cs, "test-ns")

	// First call creates
	if err := EnsureForwarderRBAC(context.Background(), c, []string{"default"}, false); err != nil {
		t.Fatal(err)
	}

	// Second call with new SA should merge
	if err := EnsureForwarderRBAC(context.Background(), c, []string{"custom-sa"}, false); err != nil {
		t.Fatal(err)
	}

	rb, err := cs.RbacV1().RoleBindings("test-ns").Get(context.Background(), "logtap-forwarder", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rb.Subjects) != 2 {
		t.Fatalf("subjects = %d, want 2 (default + custom-sa)", len(rb.Subjects))
	}
}

func TestEnsureForwarderRBAC_DryRun(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck
	c := NewClientFromInterface(cs, "test-ns")

	err := EnsureForwarderRBAC(context.Background(), c, []string{"default"}, true)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cs.RbacV1().Roles("test-ns").Get(context.Background(), "logtap-forwarder", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatal("expected role to not exist in dry-run mode")
	}
}

func TestDeleteForwarderRBAC_Success(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck
	c := NewClientFromInterface(cs, "test-ns")

	// Create first
	if err := EnsureForwarderRBAC(context.Background(), c, []string{"default"}, false); err != nil {
		t.Fatal(err)
	}

	// Delete
	if err := DeleteForwarderRBAC(context.Background(), c, false); err != nil {
		t.Fatal(err)
	}

	_, err := cs.RbacV1().Roles("test-ns").Get(context.Background(), "logtap-forwarder", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatal("expected role to be deleted")
	}
	_, err = cs.RbacV1().RoleBindings("test-ns").Get(context.Background(), "logtap-forwarder", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatal("expected rolebinding to be deleted")
	}
}

func TestDeleteForwarderRBAC_NotFound(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck
	c := NewClientFromInterface(cs, "test-ns")

	// Should not error when nothing exists
	if err := DeleteForwarderRBAC(context.Background(), c, false); err != nil {
		t.Fatal(err)
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
