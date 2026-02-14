package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/k8s"
	"github.com/ppiankov/logtap/internal/sidecar"
)

func newUntapCmd() *cobra.Command {
	var (
		deployment  string
		statefulset string
		daemonset   string
		namespace   string
		selector    string
		session     string
		all         bool
		dryRun      bool
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "untap",
		Short: "Remove logtap forwarder sidecar from workloads",
		Long:  "Untap removes logtap log-forwarding sidecar containers from Kubernetes workloads. Use --session to remove a specific session or --all to remove all sessions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUntap(untapOpts{
				deployment:  deployment,
				statefulset: statefulset,
				daemonset:   daemonset,
				namespace:   namespace,
				selector:    selector,
				session:     session,
				all:         all,
				dryRun:      dryRun,
				force:       force,
			})
		},
	}

	cmd.Flags().StringVar(&deployment, "deployment", "", "deployment name")
	cmd.Flags().StringVar(&statefulset, "statefulset", "", "statefulset name")
	cmd.Flags().StringVar(&daemonset, "daemonset", "", "daemonset name")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace (defaults to current context)")
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "label selector")
	cmd.Flags().StringVar(&session, "session", "", "session ID to remove")
	cmd.Flags().BoolVar(&all, "all", false, "remove all sessions")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show diff without applying")
	cmd.Flags().BoolVar(&force, "force", false, "required with --all to confirm bulk removal")

	return cmd
}

type untapOpts struct {
	deployment  string
	statefulset string
	daemonset   string
	namespace   string
	selector    string
	session     string
	all         bool
	dryRun      bool
	force       bool
}

func runUntap(opts untapOpts) error {
	// Validate session/all flags
	if opts.session != "" && opts.all {
		return fmt.Errorf("--session and --all are mutually exclusive")
	}
	if opts.session == "" && !opts.all {
		return fmt.Errorf("specify --session or --all")
	}
	if opts.all && !opts.dryRun && !opts.force {
		return fmt.Errorf("--all requires --force to confirm bulk removal (or use --dry-run)")
	}

	ctx := context.Background()

	c, err := k8s.NewClient(opts.namespace)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	// Discover workloads
	var workloads []*k8s.Workload

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

	if modes > 1 {
		return fmt.Errorf("specify only one of --deployment, --statefulset, --daemonset, or --selector")
	}

	if modes == 0 {
		// Auto-discover tapped workloads
		all, err := k8s.DiscoverTapped(ctx, c, sidecar.AnnotationTapped)
		if err != nil {
			return err
		}
		if opts.session != "" {
			// Filter to workloads containing this session
			for _, w := range all {
				for _, s := range sidecar.ParseSessions(w.Annotations[sidecar.AnnotationTapped]) {
					if s == opts.session {
						workloads = append(workloads, w)
						break
					}
				}
			}
		} else {
			workloads = all
		}
	} else {
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
			workloads = wl
		}
	}

	if len(workloads) == 0 {
		return fmt.Errorf("no tapped workloads found")
	}

	// Execute removal
	var totalRemoved int
	for _, w := range workloads {
		if opts.all {
			results, err := sidecar.RemoveAll(ctx, c, w, opts.dryRun)
			if err != nil {
				return fmt.Errorf("untap %s/%s: %w", w.Kind, w.Name, err)
			}
			if opts.dryRun {
				fmt.Fprintf(os.Stderr, "[dry-run] %s/%s:\n", w.Kind, w.Name)
				if len(results) > 0 {
					_, _ = fmt.Fprintln(os.Stdout, results[0].Diff)
				}
			} else {
				for _, r := range results {
					fmt.Fprintf(os.Stderr, "Untapped %s/%s (session %s)\n", w.Kind, w.Name, r.SessionID)
				}
			}
			totalRemoved += len(results)
		} else {
			result, err := sidecar.Remove(ctx, c, w, opts.session, opts.dryRun)
			if err != nil {
				return fmt.Errorf("untap %s/%s: %w", w.Kind, w.Name, err)
			}
			if opts.dryRun {
				fmt.Fprintf(os.Stderr, "[dry-run] %s/%s:\n", w.Kind, w.Name)
				_, _ = fmt.Fprintln(os.Stdout, result.Diff)
			} else {
				fmt.Fprintf(os.Stderr, "Untapped %s/%s (session %s)\n", w.Kind, w.Name, result.SessionID)
			}
			totalRemoved++
		}
	}

	if !opts.dryRun {
		fmt.Fprintf(os.Stderr, "\nRemoved %d session(s) from %d workload(s)\n", totalRemoved, len(workloads))
	}

	return nil
}
