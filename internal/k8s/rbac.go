package k8s

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RBACCheck describes a single permission to verify.
type RBACCheck struct {
	Resource string // "deployments", "pods", etc.
	Verb     string // "get", "patch", "create", "list"
	Group    string // "apps", "", etc.
}

// RBACResult pairs a check with its outcome.
type RBACResult struct {
	Check   RBACCheck
	Allowed bool
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
