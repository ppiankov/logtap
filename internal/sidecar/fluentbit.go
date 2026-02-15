package sidecar

import (
	"context"
	"fmt"

	"github.com/ppiankov/logtap/internal/k8s"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// AnnotationForwarder tracks the forwarder type used (logtap or fluent-bit).
	AnnotationForwarder = "logtap.dev/forwarder"

	// ForwarderLogtap is the default forwarder type.
	ForwarderLogtap = "logtap"

	// ForwarderFluentBit uses Fluent Bit as the sidecar forwarder.
	ForwarderFluentBit = "fluent-bit"

	fluentBitConfigVolumeName = "logtap-fb-config"
	fluentBitLogsVolumeName   = "logtap-fb-varlog"
	fluentBitLogsPath         = "/var/log/pods"
)

// FluentBitConfig generates the Fluent Bit configuration for a session.
func FluentBitConfig(target, sessionID, namespace string) string {
	return fmt.Sprintf(`[SERVICE]
    Flush        1
    Log_Level    info

[INPUT]
    Name         tail
    Path         /var/log/pods/%s_*/*/*.log
    Tag          kube.*
    Read_from_Head False
    Refresh_Interval 5

[FILTER]
    Name         modify
    Match        *
    Add          session %s

[OUTPUT]
    Name         loki
    Match        *
    Host         %s
    Labels       session=%s
    Auto_Kubernetes_Labels off
`, namespace, sessionID, target, sessionID)
}

// ConfigMapName returns the ConfigMap name for a Fluent Bit session.
func ConfigMapName(sessionID string) string {
	return "logtap-fb-" + sessionID
}

// CreateFluentBitConfigMap creates a ConfigMap with the Fluent Bit configuration.
func CreateFluentBitConfigMap(ctx context.Context, c *k8s.Client, sessionID, target string, dryRun bool) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(sessionID),
			Namespace: c.NS,
			Labels: map[string]string{
				k8s.LabelManagedBy:   k8s.ManagedByValue,
				"logtap.dev/session": sessionID,
			},
		},
		Data: map[string]string{
			"fluent-bit.conf": FluentBitConfig(target, sessionID, c.NS),
		},
	}

	if dryRun {
		return nil
	}

	_, err := c.CS.CoreV1().ConfigMaps(c.NS).Create(ctx, cm, metav1.CreateOptions{})
	return err
}

// DeleteFluentBitConfigMap removes the ConfigMap for a session.
func DeleteFluentBitConfigMap(ctx context.Context, c *k8s.Client, sessionID string, dryRun bool) error {
	if dryRun {
		return nil
	}
	return c.CS.CoreV1().ConfigMaps(c.NS).Delete(ctx, ConfigMapName(sessionID), metav1.DeleteOptions{})
}

// BuildFluentBitContainer returns a Fluent Bit sidecar container spec.
func BuildFluentBitContainer(cfg SidecarConfig) corev1.Container {
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
		Image: cfg.Image,
		Args:  []string{"/fluent-bit/bin/fluent-bit", "-c", "/fluent-bit/etc/fluent-bit.conf"},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"sh", "-c", "sleep 5"},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      fluentBitConfigVolumeName,
				MountPath: "/fluent-bit/etc",
				ReadOnly:  true,
			},
			{
				Name:      fluentBitLogsVolumeName,
				MountPath: fluentBitLogsPath,
				ReadOnly:  true,
			},
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

// FluentBitVolumes returns the volumes needed for the Fluent Bit sidecar.
func FluentBitVolumes(sessionID string) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: fluentBitConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ConfigMapName(sessionID),
					},
				},
			},
		},
		{
			Name: fluentBitLogsVolumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fluentBitLogsPath,
				},
			},
		},
	}
}

// FluentBitVolumeNames returns the volume names to remove on untap.
func FluentBitVolumeNames() []string {
	return []string{fluentBitConfigVolumeName, fluentBitLogsVolumeName}
}
