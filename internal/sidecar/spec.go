package sidecar

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	AnnotationTapped = "logtap.dev/tapped"
	AnnotationTarget = "logtap.dev/target"
	DefaultImage     = "ghcr.io/ppiankov/logtap-forwarder:latest"
	ContainerPrefix  = "logtap-forwarder-"
	DefaultMemReq    = "16Mi"
	DefaultMemLimit  = "32Mi"
	DefaultCPUReq    = "25m"
	DefaultCPULimit  = "50m"
)

// SidecarConfig holds parameters for building the forwarder sidecar container.
type SidecarConfig struct {
	SessionID  string
	Target     string
	Image      string
	Forwarder  string // "logtap" (default) or "fluent-bit"
	MemRequest string
	MemLimit   string
	CPURequest string
	CPULimit   string
}

// ContainerName returns the sidecar container name for this session.
func (c *SidecarConfig) ContainerName() string {
	return ContainerPrefix + c.SessionID
}

// BuildContainer returns a fully configured sidecar container spec.
func BuildContainer(cfg SidecarConfig) corev1.Container {
	image := cfg.Image
	if image == "" {
		image = DefaultImage
	}
	memReq := cfg.MemRequest
	if memReq == "" {
		memReq = DefaultMemReq
	}
	memLimit := cfg.MemLimit
	if memLimit == "" {
		memLimit = DefaultMemLimit
	}
	cpuReq := cfg.CPURequest
	if cpuReq == "" {
		cpuReq = DefaultCPUReq
	}
	cpuLimit := cfg.CPULimit
	if cpuLimit == "" {
		cpuLimit = DefaultCPULimit
	}

	return corev1.Container{
		Name:  cfg.ContainerName(),
		Image: image,
		Env: []corev1.EnvVar{
			{Name: "LOGTAP_TARGET", Value: cfg.Target},
			{Name: "LOGTAP_SESSION", Value: cfg.SessionID},
			{Name: "LOGTAP_POD_NAME", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			}},
			{Name: "LOGTAP_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			}},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memReq),
				corev1.ResourceCPU:    resource.MustParse(cpuReq),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memLimit),
				corev1.ResourceCPU:    resource.MustParse(cpuLimit),
			},
		},
	}
}

// Annotations returns the annotation key-value pairs for a tapped workload.
func Annotations(cfg SidecarConfig) map[string]string {
	return map[string]string{
		AnnotationTapped: cfg.SessionID,
		AnnotationTarget: cfg.Target,
	}
}

// ParseSessions splits a comma-separated annotation value into session IDs.
func ParseSessions(annotation string) []string {
	if annotation == "" {
		return nil
	}
	parts := strings.Split(annotation, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// AddSession appends a session ID to a comma-separated annotation value.
func AddSession(annotation, sessionID string) string {
	if annotation == "" {
		return sessionID
	}
	return annotation + "," + sessionID
}

// RemoveSession removes a session ID from a comma-separated annotation value.
func RemoveSession(annotation, sessionID string) string {
	sessions := ParseSessions(annotation)
	out := make([]string, 0, len(sessions))
	for _, s := range sessions {
		if s != sessionID {
			out = append(out, s)
		}
	}
	return strings.Join(out, ",")
}
