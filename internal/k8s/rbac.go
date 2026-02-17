package k8s

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	forwarderRoleName = "logtap-forwarder"
)

// RBACCheck describes a single permission to verify.
type RBACCheck struct {
	Resource string `json:"resource"` // "deployments", "pods", etc.
	Verb     string `json:"verb"`     // "get", "patch", "create", "list"
	Group    string `json:"group"`    // "apps", "", etc.
}

// RBACResult pairs a check with its outcome.
type RBACResult struct {
	Check   RBACCheck `json:"check"`
	Allowed bool      `json:"allowed"`
}

// CheckRBAC verifies a list of permissions via SelfSubjectAccessReview.
func CheckRBAC(ctx context.Context, c *Client, checks []RBACCheck) ([]RBACResult, error) {
	results := make([]RBACResult, 0, len(checks))
	for _, check := range checks {
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: c.NS,
					Verb:      check.Verb,
					Group:     check.Group,
					Resource:  check.Resource,
				},
			},
		}
		resp, err := c.CS.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("check RBAC %s/%s %s: %w", check.Group, check.Resource, check.Verb, err)
		}
		results = append(results, RBACResult{
			Check:   check,
			Allowed: resp.Status.Allowed,
		})
	}
	return results, nil
}

// EnsureForwarderRBAC creates (or updates) a Role and RoleBinding in the
// client's namespace so that the given service accounts can read pods and
// pod logs — required by the logtap-forwarder sidecar.
func EnsureForwarderRBAC(ctx context.Context, c *Client, serviceAccounts []string, dryRun bool) error {
	if dryRun {
		return nil
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      forwarderRoleName,
			Namespace: c.NS,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "pods/log"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	existing, err := c.CS.RbacV1().Roles(c.NS).Get(ctx, forwarderRoleName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = c.CS.RbacV1().Roles(c.NS).Create(ctx, role, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create role %s: %w", forwarderRoleName, err)
		}
	} else if err != nil {
		return fmt.Errorf("get role %s: %w", forwarderRoleName, err)
	} else {
		existing.Rules = role.Rules
		_, err = c.CS.RbacV1().Roles(c.NS).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update role %s: %w", forwarderRoleName, err)
		}
	}

	subjects := make([]rbacv1.Subject, len(serviceAccounts))
	for i, sa := range serviceAccounts {
		subjects[i] = rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      sa,
			Namespace: c.NS,
		}
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      forwarderRoleName,
			Namespace: c.NS,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     forwarderRoleName,
		},
		Subjects: subjects,
	}

	existingRB, err := c.CS.RbacV1().RoleBindings(c.NS).Get(ctx, forwarderRoleName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = c.CS.RbacV1().RoleBindings(c.NS).Create(ctx, rb, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create rolebinding %s: %w", forwarderRoleName, err)
		}
	} else if err != nil {
		return fmt.Errorf("get rolebinding %s: %w", forwarderRoleName, err)
	} else {
		existingRB.Subjects = mergeSubjects(existingRB.Subjects, subjects)
		_, err = c.CS.RbacV1().RoleBindings(c.NS).Update(ctx, existingRB, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update rolebinding %s: %w", forwarderRoleName, err)
		}
	}

	return nil
}

// DeleteForwarderRBAC removes the Role and RoleBinding created by EnsureForwarderRBAC.
func DeleteForwarderRBAC(ctx context.Context, c *Client, dryRun bool) error {
	if dryRun {
		return nil
	}

	err := c.CS.RbacV1().RoleBindings(c.NS).Delete(ctx, forwarderRoleName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete rolebinding %s: %w", forwarderRoleName, err)
	}

	err = c.CS.RbacV1().Roles(c.NS).Delete(ctx, forwarderRoleName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete role %s: %w", forwarderRoleName, err)
	}

	return nil
}

// mergeSubjects adds new subjects to existing, deduplicating by name+namespace.
func mergeSubjects(existing, new []rbacv1.Subject) []rbacv1.Subject {
	seen := make(map[string]bool)
	for _, s := range existing {
		seen[s.Name+"/"+s.Namespace] = true
	}
	merged := append([]rbacv1.Subject{}, existing...)
	for _, s := range new {
		key := s.Name + "/" + s.Namespace
		if !seen[key] {
			merged = append(merged, s)
			seen[key] = true
		}
	}
	return merged
}
