package contextutil

import (
	"context"
	"time"
)

// NewClusterContext creates a context with a timeout for cluster operations.
func NewClusterContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
