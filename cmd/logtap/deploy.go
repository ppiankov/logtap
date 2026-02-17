package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/logtap/internal/k8s"
)

const (
	defaultRecvImage = "ghcr.io/ppiankov/logtap:latest"
	defaultRecvPort  = int32(3100)
	defaultMaxDisk   = "10GB"
)

func newDeployCmd() *cobra.Command {
	var (
		namespace string
		image     string
		port      int32
		maxDisk   string
		cleanup   bool
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy logtap receiver in-cluster",
		Long: `Deploy creates a logtap receiver Pod and Service inside the Kubernetes cluster.
This eliminates the need for pods to reach your local machine over VPN or external networks.

After deploying, use the printed target address with 'logtap tap --target'.
Retrieve captures with 'kubectl cp'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cleanup {
				return runDeployCleanup(namespace, dryRun)
			}
			return runDeploy(deployOpts{
				namespace: namespace,
				image:     image,
				port:      port,
				maxDisk:   maxDisk,
				dryRun:    dryRun,
			})
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace (defaults to current context)")
	cmd.Flags().StringVar(&image, "image", defaultRecvImage, "receiver image")
	cmd.Flags().Int32Var(&port, "port", defaultRecvPort, "receiver listen port")
	cmd.Flags().StringVar(&maxDisk, "max-disk", defaultMaxDisk, "disk cap for in-cluster captures")
	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "remove deployed receiver resources")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be created without applying")

	return cmd
}

type deployOpts struct {
	namespace string
	image     string
	port      int32
	maxDisk   string
	dryRun    bool
}

func runDeploy(opts deployOpts) error {
	ctx, cancel := clusterContext()
	defer cancel()

	c, err := k8s.NewClient(opts.namespace)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	labels := map[string]string{
		k8s.LabelManagedBy: k8s.ManagedByValue,
		k8s.LabelName:      k8s.ReceiverName,
	}

	spec := k8s.ReceiverSpec{
		Image:     opts.image,
		Namespace: c.NS,
		PodName:   k8s.ReceiverName,
		SvcName:   k8s.ReceiverName,
		Port:      opts.port,
		Args: []string{
			"recv",
			"--headless",
			"--listen", fmt.Sprintf(":%d", opts.port),
			"--dir", "/data",
			"--max-disk", opts.maxDisk,
		},
		Labels: labels,
	}

	target := fmt.Sprintf("%s.%s.svc.cluster.local:%d", k8s.ReceiverName, c.NS, opts.port)

	if opts.dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would create in namespace %q:\n", c.NS)
		fmt.Fprintf(os.Stderr, "  Pod:     %s\n", k8s.ReceiverName)
		fmt.Fprintf(os.Stderr, "  Service: %s\n", k8s.ReceiverName)
		fmt.Fprintf(os.Stderr, "  Image:   %s\n", opts.image)
		fmt.Fprintf(os.Stderr, "  Port:    %d\n", opts.port)
		fmt.Fprintf(os.Stderr, "  MaxDisk: %s\n", opts.maxDisk)
		fmt.Fprintf(os.Stderr, "\nTarget: %s\n", target)
		return nil
	}

	fmt.Fprintf(os.Stderr, "Deploying receiver in namespace %q...\n", c.NS)

	res, err := k8s.DeployReceiver(ctx, c, spec)
	if err != nil {
		// Attempt cleanup on partial failure
		if res != nil {
			fmt.Fprintf(os.Stderr, "Partial failure, cleaning up...\n")
			_ = k8s.DeleteReceiver(ctx, c, res)
		}
		return fmt.Errorf("deploy receiver: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Waiting for receiver to be ready...\n")
	if err := k8s.WaitForPodReady(ctx, c, c.NS, k8s.ReceiverName, defaultTimeout); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v (pod may still be starting)\n", err)
	}

	fmt.Fprintf(os.Stderr, "\nReceiver deployed successfully.\n\n")
	fmt.Fprintf(os.Stderr, "Target: %s\n", target)
	fmt.Fprintf(os.Stderr, "\nUsage:\n")
	fmt.Fprintf(os.Stderr, "  logtap tap --target %s --deployment <name> -n %s\n", target, c.NS)
	fmt.Fprintf(os.Stderr, "\nRetrieve captures:\n")
	fmt.Fprintf(os.Stderr, "  kubectl cp %s/%s:/data ./capture\n", c.NS, k8s.ReceiverName)
	fmt.Fprintf(os.Stderr, "\nCleanup:\n")
	fmt.Fprintf(os.Stderr, "  logtap deploy --cleanup -n %s\n", c.NS)

	return nil
}

func runDeployCleanup(namespace string, dryRun bool) error {
	ctx, cancel := clusterContext()
	defer cancel()

	c, err := k8s.NewClient(namespace)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	res := &k8s.ReceiverResources{
		Namespace: c.NS,
		PodName:   k8s.ReceiverName,
		SvcName:   k8s.ReceiverName,
		CreatedNS: false, // never delete the namespace on cleanup
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "[dry-run] Would delete in namespace %q:\n", c.NS)
		fmt.Fprintf(os.Stderr, "  Pod:     %s\n", k8s.ReceiverName)
		fmt.Fprintf(os.Stderr, "  Service: %s\n", k8s.ReceiverName)
		return nil
	}

	fmt.Fprintf(os.Stderr, "Cleaning up receiver in namespace %q...\n", c.NS)
	if err := k8s.DeleteReceiver(ctx, c, res); err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Receiver cleaned up.\n")

	return nil
}
