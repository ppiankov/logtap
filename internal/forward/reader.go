package forward

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const containerPrefix = "logtap-forwarder-"

// LogLine is a parsed log line from a container.
type LogLine struct {
	Timestamp time.Time
	Container string
	Line      string
}

// Reader reads logs from sibling containers via the Kubernetes API.
type Reader struct {
	podName   string
	namespace string
	cs        kubernetes.Interface
}

// NewReader creates a Reader using in-cluster config.
func NewReader(podName, namespace string) (*Reader, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}
	return &Reader{podName: podName, namespace: namespace, cs: cs}, nil
}

// NewReaderFromClient creates a Reader from an existing clientset (for testing).
func NewReaderFromClient(cs kubernetes.Interface, podName, namespace string) *Reader {
	return &Reader{podName: podName, namespace: namespace, cs: cs}
}

// DiscoverContainers returns the names of sibling containers (excluding logtap-forwarder ones).
func (r *Reader) DiscoverContainers(ctx context.Context) ([]string, error) {
	pod, err := r.cs.CoreV1().Pods(r.namespace).Get(ctx, r.podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod: %w", err)
	}
	return FilterContainers(pod.Spec.Containers), nil
}

// FilterContainers returns container names that are not logtap-forwarder sidecars.
func FilterContainers(containers []corev1.Container) []string {
	var names []string
	for _, c := range containers {
		if !strings.HasPrefix(c.Name, containerPrefix) {
			names = append(names, c.Name)
		}
	}
	return names
}

// Follow streams log lines from a container, sending parsed lines to out.
// Blocks until the context is cancelled or the stream ends.
func (r *Reader) Follow(ctx context.Context, container string, out chan<- LogLine) error {
	req := r.cs.CoreV1().Pods(r.namespace).GetLogs(r.podName, &corev1.PodLogOptions{
		Container:  container,
		Follow:     true,
		Timestamps: true,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("open log stream for %s: %w", container, err)
	}
	defer func() { _ = stream.Close() }()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		ts, msg := ParseLogLine(line)
		select {
		case out <- LogLine{Timestamp: ts, Container: container, Line: msg}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return scanner.Err()
}

// ParseLogLine splits a Kubernetes timestamp-prefixed log line.
// Format: "2024-01-15T10:30:00.123456789Z actual log message"
func ParseLogLine(line string) (time.Time, string) {
	idx := strings.IndexByte(line, ' ')
	if idx < 0 {
		return time.Now(), line
	}
	ts, err := time.Parse(time.RFC3339Nano, line[:idx])
	if err != nil {
		return time.Now(), line
	}
	return ts, line[idx+1:]
}

// FollowAll discovers containers and follows each in a goroutine.
// Sends all log lines to out. Returns when context is cancelled.
func (r *Reader) FollowAll(ctx context.Context, out chan<- LogLine) error {
	containers, err := r.DiscoverContainers(ctx)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return fmt.Errorf("no sibling containers found")
	}

	errCh := make(chan error, len(containers))
	for _, name := range containers {
		go func(c string) {
			errCh <- r.followWithRetry(ctx, c, out)
		}(name)
	}

	// wait for context cancellation — individual follower errors are logged but not fatal
	<-ctx.Done()
	return nil
}

func (r *Reader) followWithRetry(ctx context.Context, container string, out chan<- LogLine) error {
	for {
		err := r.Follow(ctx, container, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && err != io.EOF {
			fmt.Printf("follow %s: %v, retrying in 2s\n", container, err)
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
