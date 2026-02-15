package sidecar

import (
	"context"
	"fmt"

	"github.com/ppiankov/logtap/internal/k8s"
)

// RemoveResult holds the outcome of a sidecar removal.
type RemoveResult struct {
	Workload  *k8s.Workload
	SessionID string
	Diff      string
	Applied   bool
}

// Remove removes a single logtap forwarder sidecar from a workload by session ID.
func Remove(ctx context.Context, c *k8s.Client, w *k8s.Workload, sessionID string, dryRun bool) (*RemoveResult, error) {
	tapped := w.Annotations[AnnotationTapped]
	sessions := ParseSessions(tapped)
	if len(sessions) == 0 {
		return nil, fmt.Errorf("workload %s/%s is not tapped", w.Kind, w.Name)
	}

	found := false
	for _, s := range sessions {
		if s == sessionID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("session %s not found in %s/%s (tapped: %s)", sessionID, w.Kind, w.Name, tapped)
	}

	isFluentBit := w.Annotations[AnnotationForwarder] == ForwarderFluentBit

	containerName := ContainerPrefix + sessionID
	newTapped := RemoveSession(tapped, sessionID)

	rs := k8s.RemovePatchSpec{
		ContainerNames: []string{containerName},
	}

	if isFluentBit {
		rs.VolumeNames = FluentBitVolumeNames()
		// Clean up ConfigMap
		_ = DeleteFluentBitConfigMap(ctx, c, sessionID, dryRun)
	}

	if newTapped == "" {
		// Last session — delete all logtap annotations
		rs.DeleteAnnotations = []string{AnnotationTapped, AnnotationTarget, AnnotationForwarder}
	} else {
		// Other sessions remain — update tapped annotation
		rs.SetAnnotations = map[string]string{AnnotationTapped: newTapped}
	}

	diff, err := k8s.RemovePatch(ctx, c, w, rs, dryRun)
	if err != nil {
		return nil, fmt.Errorf("remove patch %s/%s: %w", w.Kind, w.Name, err)
	}

	return &RemoveResult{
		Workload:  w,
		SessionID: sessionID,
		Diff:      diff,
		Applied:   !dryRun,
	}, nil
}

// RemoveAll removes all logtap forwarder sidecars from a workload in a single patch.
func RemoveAll(ctx context.Context, c *k8s.Client, w *k8s.Workload, dryRun bool) ([]*RemoveResult, error) {
	tapped := w.Annotations[AnnotationTapped]
	sessions := ParseSessions(tapped)
	if len(sessions) == 0 {
		return nil, fmt.Errorf("workload %s/%s is not tapped", w.Kind, w.Name)
	}

	isFluentBit := w.Annotations[AnnotationForwarder] == ForwarderFluentBit

	containerNames := make([]string, len(sessions))
	for i, s := range sessions {
		containerNames[i] = ContainerPrefix + s
	}

	rs := k8s.RemovePatchSpec{
		ContainerNames:    containerNames,
		DeleteAnnotations: []string{AnnotationTapped, AnnotationTarget, AnnotationForwarder},
	}

	if isFluentBit {
		rs.VolumeNames = FluentBitVolumeNames()
		for _, s := range sessions {
			_ = DeleteFluentBitConfigMap(ctx, c, s, dryRun)
		}
	}

	diff, err := k8s.RemovePatch(ctx, c, w, rs, dryRun)
	if err != nil {
		return nil, fmt.Errorf("remove all %s/%s: %w", w.Kind, w.Name, err)
	}

	results := make([]*RemoveResult, len(sessions))
	for i, s := range sessions {
		results[i] = &RemoveResult{
			Workload:  w,
			SessionID: s,
			Diff:      diff,
			Applied:   !dryRun,
		}
	}
	return results, nil
}
