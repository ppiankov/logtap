package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/k8s"
	"github.com/ppiankov/logtap/internal/sidecar"
)

// checkResult holds all check data for JSON output.
type checkResult struct {
	Cluster    *k8s.ClusterInfo   `json:"cluster,omitempty"`
	RBAC       []k8s.RBACResult   `json:"rbac,omitempty"`
	Quotas     []k8s.QuotaSummary `json:"quotas,omitempty"`
	Orphans    *k8s.OrphanResult  `json:"orphans,omitempty"`
	Candidates []*k8s.Workload    `json:"candidates,omitempty"`
}

func newCheckCmd() *cobra.Command {
	var (
		namespace  string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate cluster readiness and detect leftovers",
		Long:  "Check verifies RBAC permissions, resource quotas, and detects orphaned logtap sidecars or stale annotations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(namespace, jsonOutput)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace (defaults to current context)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func runCheck(namespace string, jsonOutput bool) error {
	ctx := context.Background()

	c, err := k8s.NewClient(namespace)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	result := &checkResult{}

	// Cluster info
	info, err := k8s.GetClusterInfo(ctx, c)
	if err != nil && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Cluster:       (error: %v)\n", err)
	} else if err == nil {
		result.Cluster = info
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Cluster:       %s\n", info.Version)
			fmt.Fprintf(os.Stderr, "Namespace:     %s\n", info.Namespace)
		}
	}

	// RBAC checks
	rbacChecks := []k8s.RBACCheck{
		{Resource: "deployments", Verb: "get", Group: "apps"},
		{Resource: "deployments", Verb: "patch", Group: "apps"},
		{Resource: "statefulsets", Verb: "get", Group: "apps"},
		{Resource: "statefulsets", Verb: "patch", Group: "apps"},
		{Resource: "daemonsets", Verb: "get", Group: "apps"},
		{Resource: "daemonsets", Verb: "patch", Group: "apps"},
		{Resource: "pods", Verb: "create", Group: ""},
		{Resource: "resourcequotas", Verb: "list", Group: ""},
		{Resource: "nodes", Verb: "list", Group: ""},
	}

	rbacResults, err := k8s.CheckRBAC(ctx, c, rbacChecks)
	if err != nil {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "RBAC:          error: %v\n", err)
		}
	} else {
		result.RBAC = rbacResults
		if !jsonOutput {
			allOK := true
			var denied []string
			for _, r := range rbacResults {
				if !r.Allowed {
					allOK = false
					denied = append(denied, fmt.Sprintf("%s %s/%s", r.Check.Verb, r.Check.Group, r.Check.Resource))
				}
			}
			if allOK {
				fmt.Fprintf(os.Stderr, "RBAC:          ok\n")
			} else {
				fmt.Fprintf(os.Stderr, "RBAC:          denied: %s\n", strings.Join(denied, ", "))
			}
		}
	}

	// Quota summary
	quotas, err := k8s.GetQuotaSummary(ctx, c)
	if err != nil {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Quota:         error: %v\n", err)
		}
	} else {
		result.Quotas = quotas
		if !jsonOutput {
			if len(quotas) == 0 {
				fmt.Fprintf(os.Stderr, "Quota:         none configured\n")
			} else {
				for _, q := range quotas {
					parts := []string{}
					if q.MemHard != "" {
						parts = append(parts, fmt.Sprintf("memory %s / %s", q.MemUsed, q.MemHard))
					}
					if q.CPUHard != "" {
						parts = append(parts, fmt.Sprintf("cpu %s / %s", q.CPUUsed, q.CPUHard))
					}
					if len(parts) > 0 {
						fmt.Fprintf(os.Stderr, "Quota:         %s (%s)\n", q.Name, strings.Join(parts, ", "))
					}
				}
			}
		}
	}

	if !jsonOutput {
		fmt.Fprintln(os.Stderr)
	}

	// Orphan detection
	checker := func(target string) bool {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://" + target + "/metrics")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return true
	}

	orphans, err := k8s.FindOrphans(ctx, c, sidecar.AnnotationTapped, sidecar.AnnotationTarget, sidecar.ContainerPrefix, checker)
	if err != nil {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Leftovers:     error: %v\n", err)
		}
	} else {
		result.Orphans = orphans
		if !jsonOutput {
			if len(orphans.Sidecars) == 0 && len(orphans.StaleWorkloads) == 0 && len(orphans.Receivers) == 0 {
				fmt.Fprintf(os.Stderr, "Leftovers:     none\n")
			} else {
				fmt.Fprintf(os.Stderr, "Leftovers:\n")
				for _, s := range orphans.Sidecars {
					status := "receiver reachable"
					if !s.TargetReachable {
						status = "receiver unreachable"
					}
					fmt.Fprintf(os.Stderr, "  ! %s/%-20s has logtap sidecar (%s)\n", s.Workload.Kind, s.Workload.Name, status)
				}
				for _, s := range orphans.StaleWorkloads {
					fmt.Fprintf(os.Stderr, "  ! %s/%-20s stale annotation (no sidecar container)\n", s.Workload.Kind, s.Workload.Name)
				}
				for _, r := range orphans.Receivers {
					age := r.Age.Truncate(time.Minute)
					fmt.Fprintf(os.Stderr, "  ! Pod %-24s in namespace %s (age: %s)\n", r.PodName, r.Namespace, age)
				}
				fmt.Fprintln(os.Stderr)
				if len(orphans.Sidecars) > 0 {
					fmt.Fprintf(os.Stderr, "  Run: logtap untap --all --force    to remove sidecars\n")
				}
				if len(orphans.Receivers) > 0 {
					fmt.Fprintf(os.Stderr, "  Run: kubectl delete pod,svc -n %s -l %s=%s    to remove receiver\n", c.NS, k8s.LabelManagedBy, k8s.ManagedByValue)
				}
			}
		}
	}

	if !jsonOutput {
		fmt.Fprintln(os.Stderr)
	}

	// Candidate workloads (not tapped)
	all, err := k8s.DiscoverBySelector(ctx, c, "")
	if err != nil {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Candidates:    error: %v\n", err)
		}
	} else {
		tapped, _ := k8s.DiscoverTapped(ctx, c, sidecar.AnnotationTapped)
		tappedSet := make(map[string]bool, len(tapped))
		for _, w := range tapped {
			tappedSet[string(w.Kind)+"/"+w.Name] = true
		}

		for _, w := range all {
			key := string(w.Kind) + "/" + w.Name
			if !tappedSet[key] {
				result.Candidates = append(result.Candidates, w)
			}
		}

		if !jsonOutput {
			if len(result.Candidates) == 0 {
				fmt.Fprintf(os.Stderr, "Candidate workloads: none found\n")
			} else {
				fmt.Fprintf(os.Stderr, "Candidate workloads:\n")
				for _, w := range result.Candidates {
					mem := w.Replicas * 16 // default 16Mi per sidecar
					fmt.Fprintf(os.Stderr, "  %s/%-24s %d replicas   +%dMi for sidecar\n", w.Kind, w.Name, w.Replicas, mem)
				}
			}
		}
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	return nil
}
