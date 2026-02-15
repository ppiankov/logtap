package sidecar

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/ppiankov/logtap/internal/k8s"
)

// InjectResult holds the outcome of a sidecar injection.
type InjectResult struct {
	Workload  *k8s.Workload
	SessionID string
	Diff      string
	Applied   bool
}

// Inject adds a logtap forwarder sidecar to a workload.
// If dryRun is true, the diff is computed but no changes are applied.
func Inject(ctx context.Context, c *k8s.Client, w *k8s.Workload, cfg SidecarConfig, dryRun bool) (*InjectResult, error) {
	// Check if already tapped with this session
	tapped := w.Annotations[AnnotationTapped]
	for _, s := range ParseSessions(tapped) {
		if s == cfg.SessionID {
			return nil, fmt.Errorf("workload %s/%s already tapped with session %s", w.Kind, w.Name, cfg.SessionID)
		}
	}

	// Check for existing sidecar container with same name
	containers := getContainerNames(w)
	containerName := cfg.ContainerName()
	for _, name := range containers {
		if name == containerName {
			return nil, fmt.Errorf("container %q already exists in %s/%s", containerName, w.Kind, w.Name)
		}
	}

	// Update annotation to include new session
	newTapped := AddSession(tapped, cfg.SessionID)
	annotations := map[string]string{
		AnnotationTapped: newTapped,
		AnnotationTarget: cfg.Target,
	}

	var container corev1.Container
	var volumes []corev1.Volume

	if cfg.Forwarder == ForwarderFluentBit {
		if cfg.Image == "" {
			return nil, fmt.Errorf("--image is required when using --forwarder fluent-bit")
		}
		annotations[AnnotationForwarder] = ForwarderFluentBit
		container = BuildFluentBitContainer(cfg)
		volumes = FluentBitVolumes(cfg.SessionID)

		if err := CreateFluentBitConfigMap(ctx, c, cfg.SessionID, cfg.Target, dryRun); err != nil {
			return nil, fmt.Errorf("create fluent-bit configmap: %w", err)
		}
	} else {
		container = BuildContainer(cfg)
	}

	ps := k8s.PatchSpec{
		Container:   container,
		Volumes:     volumes,
		Annotations: annotations,
	}

	diff, err := k8s.ApplyPatch(ctx, c, w, ps, dryRun)
	if err != nil {
		return nil, fmt.Errorf("apply patch to %s/%s: %w", w.Kind, w.Name, err)
	}

	return &InjectResult{
		Workload:  w,
		SessionID: cfg.SessionID,
		Diff:      diff,
		Applied:   !dryRun,
	}, nil
}

func getContainerNames(w *k8s.Workload) []string {
	// Extract container names from the raw YAML representation
	diff := fmt.Sprintf("%v", w.Raw)
	_ = diff
	// Parse from annotations - workload annotations track containers indirectly.
	// For proper checking, look at the tapped annotation sessions.
	tapped := w.Annotations[AnnotationTapped]
	sessions := ParseSessions(tapped)
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = ContainerPrefix + s
	}
	// Also check for the well-known prefix pattern in existing annotations
	for k, v := range w.Annotations {
		if strings.HasPrefix(k, ContainerPrefix) {
			names = append(names, v)
		}
	}
	return names
}
