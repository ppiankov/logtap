package main

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

// validateQuantity checks that a resource quantity string is valid Kubernetes format.
// This catches invalid values early, before they reach resource.MustParse (which panics).
func validateQuantity(flag, value string) error {
	if value == "" {
		return nil
	}
	if _, err := resource.ParseQuantity(value); err != nil {
		return fmt.Errorf("invalid %s %q: must be a valid Kubernetes resource quantity (e.g. 16Mi, 25m, 100m, 64Mi)", flag, value)
	}
	return nil
}
