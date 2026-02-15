package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelName      = "app.kubernetes.io/name"
	ManagedByValue = "logtap"
	ReceiverName   = "logtap-receiver"
)

// ReceiverSpec describes the in-cluster receiver Pod and Service.
type ReceiverSpec struct {
	Image     string
	Namespace string
	PodName   string
	SvcName   string
	Port      int32
	Args      []string
	Labels    map[string]string
	TTL       time.Duration // pod activeDeadlineSeconds; 0 means no limit
}

// ReceiverResources tracks what was created for cleanup.
type ReceiverResources struct {
	Namespace string
	PodName   string
	SvcName   string
	CreatedNS bool
}

// DeployReceiver creates namespace (if needed), Service, and Pod for in-cluster receiver.
func DeployReceiver(ctx context.Context, c *Client, spec ReceiverSpec) (*ReceiverResources, error) {
	res := &ReceiverResources{
		Namespace: spec.Namespace,
		PodName:   spec.PodName,
		SvcName:   spec.SvcName,
	}

	createdNS, err := ensureNamespace(ctx, c, spec.Namespace, spec.Labels)
	if err != nil {
		return nil, fmt.Errorf("ensure namespace %s: %w", spec.Namespace, err)
	}
	res.CreatedNS = createdNS

	svc := buildReceiverService(spec)
	if _, err := c.CS.CoreV1().Services(spec.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		return res, fmt.Errorf("create service %s: %w", spec.SvcName, err)
	}

	pod := buildReceiverPod(spec)
	if _, err := c.CS.CoreV1().Pods(spec.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return res, fmt.Errorf("create pod %s: %w", spec.PodName, err)
	}

	return res, nil
}

// WaitForPodReady polls until the pod has a Ready condition or timeout.
func WaitForPodReady(ctx context.Context, c *Client, ns, name string, timeout time.Duration) error {
	deadline := time.After(timeout)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("timeout waiting for pod %s/%s to be ready", ns, name)
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			pod, err := c.CS.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					return nil
				}
			}
		}
	}
}

// DeleteReceiver removes the Pod, Service, and optionally namespace.
func DeleteReceiver(ctx context.Context, c *Client, res *ReceiverResources) error {
	var firstErr error

	if err := c.CS.CoreV1().Pods(res.Namespace).Delete(ctx, res.PodName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		firstErr = fmt.Errorf("delete pod %s: %w", res.PodName, err)
	}

	if err := c.CS.CoreV1().Services(res.Namespace).Delete(ctx, res.SvcName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		if firstErr == nil {
			firstErr = fmt.Errorf("delete service %s: %w", res.SvcName, err)
		}
	}

	if res.CreatedNS {
		if err := c.CS.CoreV1().Namespaces().Delete(ctx, res.Namespace, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			if firstErr == nil {
				firstErr = fmt.Errorf("delete namespace %s: %w", res.Namespace, err)
			}
		}
	}

	return firstErr
}

func ensureNamespace(ctx context.Context, c *Client, ns string, labels map[string]string) (bool, error) {
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ns,
			Labels: labels,
		},
	}
	_, err := c.CS.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func buildReceiverPod(spec ReceiverSpec) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.PodName,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "receiver",
					Image: spec.Image,
					Args:  spec.Args,
					Ports: []corev1.ContainerPort{
						{ContainerPort: spec.Port, Protocol: corev1.ProtocolTCP},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/healthz",
								Port: intstr.FromInt32(spec.Port),
							},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       10,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/readyz",
								Port: intstr.FromInt32(spec.Port),
							},
						},
						InitialDelaySeconds: 2,
						PeriodSeconds:       5,
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "data", MountPath: "/data"},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name:         "data",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	if spec.TTL > 0 {
		ttlSec := int64(spec.TTL.Seconds())
		pod.Spec.ActiveDeadlineSeconds = &ttlSec
	}

	return pod
}

func buildReceiverService(spec ReceiverSpec) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.SvcName,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: spec.Labels,
			Ports: []corev1.ServicePort{
				{
					Port:       spec.Port,
					TargetPort: intstr.FromInt32(spec.Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
