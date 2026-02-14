package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceWarning describes a potential resource issue.
type ResourceWarning struct {
	Level   string // "warn"
	Check   string // "quota", "limitrange", "capacity"
	Message string
}

// CheckResources inspects namespace quotas, limit ranges, and node capacity
// for potential issues when adding a sidecar.
func CheckResources(ctx context.Context, c *Client, replicas int32, memReq, cpuReq string) ([]ResourceWarning, error) {
	var warnings []ResourceWarning

	qw, err := checkQuotas(ctx, c, replicas, memReq, cpuReq)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, qw...)

	lw, err := checkLimitRanges(ctx, c, memReq, cpuReq)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, lw...)

	nw, err := checkNodeCapacity(ctx, c)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, nw...)

	return warnings, nil
}

// QuotaSummary describes a single ResourceQuota's usage for display.
type QuotaSummary struct {
	Name    string
	MemHard string
	MemUsed string
	CPUHard string
	CPUUsed string
}

// GetQuotaSummary returns structured quota data for the namespace.
func GetQuotaSummary(ctx context.Context, c *Client) ([]QuotaSummary, error) {
	quotas, err := c.CS.CoreV1().ResourceQuotas(c.NS).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list quotas: %w", err)
	}

	var summaries []QuotaSummary
	for _, q := range quotas.Items {
		s := QuotaSummary{Name: q.Name}
		if v, ok := q.Status.Hard[corev1.ResourceRequestsMemory]; ok {
			s.MemHard = v.String()
		}
		if v, ok := q.Status.Used[corev1.ResourceRequestsMemory]; ok {
			s.MemUsed = v.String()
		}
		if v, ok := q.Status.Hard[corev1.ResourceRequestsCPU]; ok {
			s.CPUHard = v.String()
		}
		if v, ok := q.Status.Used[corev1.ResourceRequestsCPU]; ok {
			s.CPUUsed = v.String()
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

// prodLabels defines the label keys and values that indicate a production namespace.
var prodLabels = map[string][]string{
	"env":             {"prod", "production"},
	"environment":     {"prod", "production"},
	"logtap.dev/prod": {"true"},
}

// IsProdNamespace checks if the current namespace has labels indicating production.
func IsProdNamespace(ctx context.Context, c *Client) (bool, error) {
	ns, err := c.CS.CoreV1().Namespaces().Get(ctx, c.NS, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get namespace %s: %w", c.NS, err)
	}

	for key, vals := range prodLabels {
		if v, ok := ns.Labels[key]; ok {
			for _, pv := range vals {
				if v == pv {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func checkQuotas(ctx context.Context, c *Client, replicas int32, memReq, cpuReq string) ([]ResourceWarning, error) {
	quotas, err := c.CS.CoreV1().ResourceQuotas(c.NS).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list quotas: %w", err)
	}

	var warnings []ResourceWarning
	sidecarMem := resource.MustParse(memReq)
	sidecarCPU := resource.MustParse(cpuReq)

	for _, q := range quotas.Items {
		// Check memory
		if hard, ok := q.Status.Hard[corev1.ResourceRequestsMemory]; ok {
			used := q.Status.Used[corev1.ResourceRequestsMemory]
			needed := sidecarMem.DeepCopy()
			needed.SetMilli(needed.MilliValue() * int64(replicas))
			total := used.DeepCopy()
			total.Add(needed)
			if total.Cmp(hard) > 0 {
				warnings = append(warnings, ResourceWarning{
					Level:   "warn",
					Check:   "quota",
					Message: fmt.Sprintf("memory quota %q: used %s + sidecar %s × %d replicas exceeds hard limit %s", q.Name, used.String(), sidecarMem.String(), replicas, hard.String()),
				})
			}
		}

		// Check CPU
		if hard, ok := q.Status.Hard[corev1.ResourceRequestsCPU]; ok {
			used := q.Status.Used[corev1.ResourceRequestsCPU]
			needed := sidecarCPU.DeepCopy()
			needed.SetMilli(needed.MilliValue() * int64(replicas))
			total := used.DeepCopy()
			total.Add(needed)
			if total.Cmp(hard) > 0 {
				warnings = append(warnings, ResourceWarning{
					Level:   "warn",
					Check:   "quota",
					Message: fmt.Sprintf("cpu quota %q: used %s + sidecar %s × %d replicas exceeds hard limit %s", q.Name, used.String(), sidecarCPU.String(), replicas, hard.String()),
				})
			}
		}
	}

	return warnings, nil
}

func checkLimitRanges(ctx context.Context, c *Client, memReq, cpuReq string) ([]ResourceWarning, error) {
	ranges, err := c.CS.CoreV1().LimitRanges(c.NS).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list limitranges: %w", err)
	}

	var warnings []ResourceWarning
	sidecarMem := resource.MustParse(memReq)
	sidecarCPU := resource.MustParse(cpuReq)

	for _, lr := range ranges.Items {
		for _, item := range lr.Spec.Limits {
			if item.Type != corev1.LimitTypeContainer {
				continue
			}
			if maxMem, ok := item.Max[corev1.ResourceMemory]; ok {
				if sidecarMem.Cmp(maxMem) > 0 {
					warnings = append(warnings, ResourceWarning{
						Level:   "warn",
						Check:   "limitrange",
						Message: fmt.Sprintf("limitrange %q: sidecar memory %s exceeds container max %s", lr.Name, sidecarMem.String(), maxMem.String()),
					})
				}
			}
			if maxCPU, ok := item.Max[corev1.ResourceCPU]; ok {
				if sidecarCPU.Cmp(maxCPU) > 0 {
					warnings = append(warnings, ResourceWarning{
						Level:   "warn",
						Check:   "limitrange",
						Message: fmt.Sprintf("limitrange %q: sidecar cpu %s exceeds container max %s", lr.Name, sidecarCPU.String(), maxCPU.String()),
					})
				}
			}
		}
	}

	return warnings, nil
}

func checkNodeCapacity(ctx context.Context, c *Client) ([]ResourceWarning, error) {
	nodes, err := c.CS.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	if len(nodes.Items) == 0 {
		return nil, nil
	}

	var warnings []ResourceWarning
	for _, node := range nodes.Items {
		allocatable := node.Status.Allocatable
		if allocatable == nil {
			continue
		}
		// Check if node reports high allocation via conditions
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeMemoryPressure && cond.Status == corev1.ConditionTrue {
				warnings = append(warnings, ResourceWarning{
					Level:   "warn",
					Check:   "capacity",
					Message: fmt.Sprintf("node %s: memory pressure detected", node.Name),
				})
			}
			if cond.Type == corev1.NodeDiskPressure && cond.Status == corev1.ConditionTrue {
				warnings = append(warnings, ResourceWarning{
					Level:   "warn",
					Check:   "capacity",
					Message: fmt.Sprintf("node %s: disk pressure detected", node.Name),
				})
			}
		}
	}

	return warnings, nil
}
