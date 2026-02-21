package contextutil

import (
	"testing"
	"time"
)

func TestNewClusterContext(t *testing.T) {
	ctx, cancel := NewClusterContext(5 * time.Second)
	defer cancel()

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}
	if time.Until(deadline) > 5*time.Second {
		t.Fatalf("deadline too far in the future: %v", deadline)
	}
	if time.Until(deadline) < 4*time.Second {
		t.Fatalf("deadline too close: %v", deadline)
	}
}

func TestNewClusterContext_Cancel(t *testing.T) {
	ctx, cancel := NewClusterContext(time.Minute)
	cancel()

	select {
	case <-ctx.Done():
		// expected
	default:
		t.Fatal("expected context to be done after cancel")
	}
}
