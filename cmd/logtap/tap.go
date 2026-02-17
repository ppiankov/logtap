package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/k8s"
	"github.com/ppiankov/logtap/internal/sidecar"
)

func newTapCmd() *cobra.Command {
	var (
		deployment    string
		statefulset   string
		daemonset     string
		namespace     string
		selector      string
		all           bool
		target        string
		forwarder     string
		dryRun        bool
		force         bool
		allowProd     bool
		image         string
		sidecarMemory string
		sidecarCPU    string
		noRollback    bool
	)

	cmd := &cobra.Command{
		Use:   "tap",
		Short: "Inject logtap forwarder sidecar into workloads",
		Long:  "Tap patches Kubernetes workloads to add a logtap log-forwarding sidecar container. The sidecar sends logs to the logtap receiver via the Loki push API.",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			applyConfigDefaults(cmd)
			if err := validateQuantity("--sidecar-memory", sidecarMemory); err != nil {
				return err
			}
			if err := validateQuantity("--sidecar-cpu", sidecarCPU); err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTap(tapOpts{
				deployment:    deployment,
				statefulset:   statefulset,
				daemonset:     daemonset,
				namespace:     namespace,
				selector:      selector,
				all:           all,
				target:        target,
				forwarder:     forwarder,
				dryRun:        dryRun,
				force:         force,
				allowProd:     allowProd,
				image:         image,
				sidecarMemory: sidecarMemory,
				sidecarCPU:    sidecarCPU,
				noRollback:    noRollback,
			})
		},
	}

	cmd.Flags().StringVar(&deployment, "deployment", "", "deployment name")
	cmd.Flags().StringVar(&statefulset, "statefulset", "", "statefulset name")
	cmd.Flags().StringVar(&daemonset, "daemonset", "", "daemonset name")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace (defaults to current context)")
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "label selector")
	cmd.Flags().BoolVar(&all, "all", false, "tap all workloads in namespace (requires --force)")
	cmd.Flags().StringVar(&target, "target", "", "receiver address (required)")
	cmd.Flags().StringVar(&forwarder, "forwarder", sidecar.ForwarderLogtap, "forwarder type (logtap or fluent-bit)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show diff without applying")
	cmd.Flags().BoolVar(&force, "force", false, "proceed despite warnings")
	cmd.Flags().BoolVar(&allowProd, "allow-prod", false, "allow tapping production namespaces")
	cmd.Flags().StringVar(&image, "image", sidecar.DefaultImage, "forwarder sidecar image")
	cmd.Flags().StringVar(&sidecarMemory, "sidecar-memory", sidecar.DefaultMemReq, "sidecar memory request (limit = 2x)")
	cmd.Flags().StringVar(&sidecarCPU, "sidecar-cpu", sidecar.DefaultCPUReq, "sidecar CPU request (limit = 2x)")
	cmd.Flags().BoolVar(&noRollback, "no-rollback", false, "disable auto-rollback on partial failure")
	_ = cmd.MarkFlagRequired("target")

	return cmd
}

type tapOpts struct {
	deployment    string
	statefulset   string
	daemonset     string
	namespace     string
	selector      string
	all           bool
	target        string
	forwarder     string
	dryRun        bool
	force         bool
	allowProd     bool
	image         string
	sidecarMemory string
	sidecarCPU    string
	noRollback    bool
}

func runTap(opts tapOpts) error {
	// Validate targeting mode: exactly one of deployment/statefulset/daemonset/selector/all
	modes := 0
	if opts.deployment != "" {
		modes++
	}
	if opts.statefulset != "" {
		modes++
	}
	if opts.daemonset != "" {
		modes++
	}
	if opts.selector != "" {
		modes++
	}
	if opts.all {
		modes++
	}
	if modes == 0 {
		return fmt.Errorf("specify one of --deployment, --statefulset, --daemonset, --selector, or --all")
	}
	if modes > 1 {
		return fmt.Errorf("specify only one of --deployment, --statefulset, --daemonset, --selector, or --all")
	}
	if opts.all && !opts.dryRun && !opts.force {
		return fmt.Errorf("--all requires --force to confirm bulk tapping (or use --dry-run)")
	}
	if opts.forwarder != sidecar.ForwarderLogtap && opts.forwarder != sidecar.ForwarderFluentBit {
		return fmt.Errorf("--forwarder must be %q or %q", sidecar.ForwarderLogtap, sidecar.ForwarderFluentBit)
	}
	if opts.forwarder == sidecar.ForwarderFluentBit && opts.image == sidecar.DefaultImage {
		return fmt.Errorf("--image is required when using --forwarder fluent-bit (no default Fluent Bit image)")
	}

	ctx, cancel := clusterContext()
	defer cancel()

	// Build k8s client
	c, err := k8s.NewClient(opts.namespace)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	// Prod namespace protection
	isProd, err := k8s.IsProdNamespace(ctx, c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check namespace labels: %v\n", err)
	}
	if isProd && !opts.allowProd {
		return fmt.Errorf("namespace %q appears to be production (use --allow-prod to override)", c.NS)
	}
	if isProd && opts.allowProd {
		fmt.Fprintf(os.Stderr, "WARNING: tapping production namespace %q\n", c.NS)
	}

	// Pre-check receiver reachability
	if !opts.force {
		if err := checkReceiver(opts.target); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: receiver not reachable: %v (use --force to proceed)\n", err)
			return fmt.Errorf("receiver pre-check failed (use --force to proceed): %w", err)
		}
	}

	// Discover workloads
	var workloads []*k8s.Workload
	switch {
	case opts.deployment != "":
		w, err := k8s.DiscoverByName(ctx, c, k8s.KindDeployment, opts.deployment)
		if err != nil {
			return err
		}
		workloads = []*k8s.Workload{w}
	case opts.statefulset != "":
		w, err := k8s.DiscoverByName(ctx, c, k8s.KindStatefulSet, opts.statefulset)
		if err != nil {
			return err
		}
		workloads = []*k8s.Workload{w}
	case opts.daemonset != "":
		w, err := k8s.DiscoverByName(ctx, c, k8s.KindDaemonSet, opts.daemonset)
		if err != nil {
			return err
		}
		workloads = []*k8s.Workload{w}
	case opts.selector != "":
		wl, err := k8s.DiscoverBySelector(ctx, c, opts.selector)
		if err != nil {
			return err
		}
		if len(wl) == 0 {
			return fmt.Errorf("no workloads found matching selector %q", opts.selector)
		}
		workloads = wl
	case opts.all:
		wl, err := k8s.DiscoverBySelector(ctx, c, "")
		if err != nil {
			return err
		}
		// Filter out already-tapped workloads
		for _, w := range wl {
			if w.Annotations[sidecar.AnnotationTapped] == "" {
				workloads = append(workloads, w)
			}
		}
		if len(workloads) == 0 {
			return fmt.Errorf("no untapped workloads found in namespace %q", c.NS)
		}
	}

	// Ensure RBAC for forwarder sidecar
	saSet := make(map[string]bool)
	for _, w := range workloads {
		saSet[k8s.ServiceAccountName(w)] = true
	}
	serviceAccounts := make([]string, 0, len(saSet))
	for sa := range saSet {
		serviceAccounts = append(serviceAccounts, sa)
	}
	if err := k8s.EnsureForwarderRBAC(ctx, c, serviceAccounts, opts.dryRun); err != nil {
		return fmt.Errorf("ensure forwarder RBAC: %w", err)
	}

	// Generate session ID
	sessionID, err := sidecar.GenerateSessionID()
	if err != nil {
		return err
	}

	// Compute resource amounts (limit = 2x request)
	memLimit := doubleResource(opts.sidecarMemory)
	cpuLimit := doubleResource(opts.sidecarCPU)

	// Resource pre-checks
	if !opts.force {
		for _, w := range workloads {
			warnings, err := k8s.CheckResources(ctx, c, w.Replicas, opts.sidecarMemory, opts.sidecarCPU)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: resource check failed: %v\n", err)
			}
			for _, warn := range warnings {
				fmt.Fprintf(os.Stderr, "Warning [%s]: %s\n", warn.Check, warn.Message)
			}
		}
	}

	// Build sidecar config
	scfg := sidecar.SidecarConfig{
		SessionID:  sessionID,
		Target:     opts.target,
		Image:      opts.image,
		Forwarder:  opts.forwarder,
		MemRequest: opts.sidecarMemory,
		MemLimit:   memLimit,
		CPURequest: opts.sidecarCPU,
		CPULimit:   cpuLimit,
	}

	// Inject into each workload with progress and rollback
	var tapped []*k8s.Workload
	total := len(workloads)

	for i, w := range workloads {
		if !opts.dryRun && total > 1 {
			fmt.Fprintf(os.Stderr, "Tapping %s/%s [%d/%d]...\n", w.Kind, w.Name, i+1, total)
		}

		result, err := sidecar.Inject(ctx, c, w, scfg, opts.dryRun)
		if err != nil {
			if !opts.dryRun && !opts.noRollback && len(tapped) > 0 {
				rollbackTap(ctx, c, tapped, sessionID)
			}
			return fmt.Errorf("inject %s/%s: %w", w.Kind, w.Name, err)
		}

		if opts.dryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] %s/%s:\n", w.Kind, w.Name)
			_, _ = fmt.Fprintln(os.Stdout, result.Diff)
			fmt.Fprintf(os.Stderr, "  Note: ensure terminationGracePeriodSeconds >= 10 for graceful sidecar drain\n")
		} else {
			tapped = append(tapped, w)
			fmt.Fprintf(os.Stderr, "Tapped %s/%s (session %s)\n", w.Kind, w.Name, sessionID)
		}
	}

	if !opts.dryRun {
		fmt.Fprintf(os.Stderr, "\nSession: %s\n", sessionID)
		fmt.Fprintf(os.Stderr, "Target:  %s\n", opts.target)
		fmt.Fprintf(os.Stderr, "Use 'logtap untap --session %s' to remove\n", sessionID)
	}

	return nil
}

func rollbackTap(ctx context.Context, c *k8s.Client, tapped []*k8s.Workload, sessionID string) {
	fmt.Fprintf(os.Stderr, "\nRolling back %d tapped workload(s)...\n", len(tapped))
	for _, w := range tapped {
		fmt.Fprintf(os.Stderr, "Rolling back: untapping %s/%s...\n", w.Kind, w.Name)
		if _, err := sidecar.Remove(ctx, c, w, sessionID, false); err != nil {
			fmt.Fprintf(os.Stderr, "  rollback failed for %s/%s: %v\n", w.Kind, w.Name, err)
		}
	}
	fmt.Fprintln(os.Stderr, "Rollback complete")
}

func checkReceiver(target string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + target + "/metrics")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func doubleResource(req string) string {
	// Simple heuristic: parse numeric prefix and double it
	// Works for "16Mi", "25m", "100m", "64Mi", etc.
	var num int
	var suffix string
	n, _ := fmt.Sscanf(req, "%d%s", &num, &suffix)
	if n >= 1 {
		return fmt.Sprintf("%d%s", num*2, suffix)
	}
	return req
}
