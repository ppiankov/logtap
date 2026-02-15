package main

import (
	"bufio"
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

func newStatusCmd() *cobra.Command {
	var (
		namespace  string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show tapped workloads and receiver stats",
		Long:  "Status lists all workloads with active logtap sidecars, pod health, and receiver throughput if reachable.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(namespace, jsonOutput)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace (defaults to current context)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func runStatus(namespace string, jsonOutput bool) error {
	ctx, cancel := clusterContext()
	defer cancel()

	c, err := k8s.NewClient(namespace)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	statuses, err := k8s.GetTappedStatus(ctx, c, sidecar.AnnotationTapped, sidecar.AnnotationTarget, sidecar.ContainerPrefix)
	if err != nil {
		return err
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(statuses)
	}

	if len(statuses) == 0 {
		fmt.Fprintln(os.Stderr, "No tapped workloads found")
		return nil
	}

	// Collect unique targets for metrics
	targets := make(map[string]bool)
	for _, s := range statuses {
		if s.Target != "" {
			targets[s.Target] = true
		}
	}

	// Fetch receiver metrics (best-effort)
	var metrics *receiverMetrics
	for target := range targets {
		m := fetchReceiverMetrics(target)
		if m != nil {
			metrics = m
			fmt.Fprintf(os.Stderr, "Receiver:      %s (reachable)\n", target)
			break
		}
		fmt.Fprintf(os.Stderr, "Receiver:      %s (not reachable)\n", target)
	}

	if metrics != nil {
		if metrics.logsReceived != "" {
			fmt.Fprintf(os.Stderr, "Logs received: %s\n", metrics.logsReceived)
		}
		if metrics.diskUsage != "" {
			fmt.Fprintf(os.Stderr, "Disk used:     %s\n", metrics.diskUsage)
		}
		if metrics.dropped != "" {
			fmt.Fprintf(os.Stderr, "Dropped:       %s\n", metrics.dropped)
		}
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Tapped workloads:")
	for _, s := range statuses {
		sessions := strings.Join(s.Sessions, ",")
		fmt.Fprintf(os.Stderr, "  %s/%-24s (%s)     %d/%d pods forwarding   sessions: %s\n",
			s.Workload.Kind, s.Workload.Name, s.Workload.Namespace, s.Ready, s.Total, sessions)
	}

	return nil
}

type receiverMetrics struct {
	logsReceived string
	diskUsage    string
	dropped      string
}

func fetchReceiverMetrics(target string) *receiverMetrics {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + target + "/metrics")
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	m := &receiverMetrics{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "logtap_logs_received_total "):
			m.logsReceived = strings.TrimPrefix(line, "logtap_logs_received_total ")
		case strings.HasPrefix(line, "logtap_disk_usage_bytes "):
			m.diskUsage = strings.TrimPrefix(line, "logtap_disk_usage_bytes ")
		case strings.HasPrefix(line, "logtap_logs_dropped_total "):
			m.dropped = strings.TrimPrefix(line, "logtap_logs_dropped_total ")
		}
	}
	return m
}
