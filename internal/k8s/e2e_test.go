package k8s_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ppiankov/logtap/internal/k8s"
	"github.com/ppiankov/logtap/internal/sidecar"
)

// TestE2E runs end-to-end tests with real forwarder and receiver images in Kind.
// Requires LOGTAP_E2E=1, LOGTAP_FORWARDER_IMAGE, and LOGTAP_RECEIVER_IMAGE.
func TestE2E(t *testing.T) {
	if os.Getenv("LOGTAP_E2E") == "" {
		t.Skip("set LOGTAP_E2E=1 to run E2E tests")
	}

	forwarderImage := os.Getenv("LOGTAP_FORWARDER_IMAGE")
	receiverImage := os.Getenv("LOGTAP_RECEIVER_IMAGE")
	if forwarderImage == "" || receiverImage == "" {
		t.Fatal("LOGTAP_FORWARDER_IMAGE and LOGTAP_RECEIVER_IMAGE must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := k8s.NewClient("")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Create isolated test namespace.
	testNS := fmt.Sprintf("logtap-e2e-%d", time.Now().UnixMilli()%100000)
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNS},
	}
	_, err = client.CS.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create namespace: %v", err)
	}

	// Create RBAC for forwarder: pods + pods/log access.
	crName := "logtap-e2e-forwarder-" + testNS
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: crName},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "pods/log"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
	_, err = client.CS.RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create ClusterRole: %v", err)
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "logtap-e2e-forwarder",
			Namespace: testNS,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     crName,
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: "default", Namespace: testNS},
		},
	}
	_, err = client.CS.RbacV1().RoleBindings(testNS).Create(ctx, rb, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create RoleBinding: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_ = client.CS.RbacV1().ClusterRoles().Delete(bg, crName, metav1.DeleteOptions{})
		_ = client.CS.CoreV1().Namespaces().Delete(bg, testNS, metav1.DeleteOptions{})
	})

	nsClient := k8s.NewClientFromInterface(client.CS, testNS)

	t.Run("LogFlow", func(t *testing.T) {
		// Deploy receiver with real image.
		labels := map[string]string{
			k8s.LabelManagedBy: k8s.ManagedByValue,
			k8s.LabelName:      k8s.ReceiverName,
		}
		spec := k8s.ReceiverSpec{
			Image:     receiverImage,
			Namespace: testNS,
			PodName:   "logtap-recv",
			SvcName:   "logtap-recv",
			Port:      3100,
			Args:      []string{"recv", "--headless", "--dir", "/data", "--listen", ":3100"},
			Labels:    labels,
		}
		_, err := k8s.DeployReceiver(ctx, nsClient, spec)
		if err != nil {
			t.Fatalf("DeployReceiver: %v", err)
		}
		t.Log("receiver deployed, waiting for ready...")

		// Wait for receiver to become ready (real image serves /healthz and /readyz).
		if err := k8s.WaitForPodReady(ctx, nsClient, testNS, "logtap-recv", 3*time.Minute); err != nil {
			t.Fatalf("WaitForPodReady: %v", err)
		}
		t.Log("receiver ready")

		// Create log-generating deployment.
		replicas := int32(1)
		logGen := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "log-generator",
				Namespace: testNS,
				Labels:    map[string]string{"app": "log-generator"},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "log-generator"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "log-generator"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "logger",
								Image:   "busybox:1.36",
								Command: []string{"sh", "-c", `i=0; while true; do echo "logtap-e2e line=$i"; i=$((i+1)); sleep 0.5; done`},
							},
						},
					},
				},
			},
		}
		_, err = client.CS.AppsV1().Deployments(testNS).Create(ctx, logGen, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create log-generator: %v", err)
		}
		waitForDeployment(t, ctx, client, testNS, "log-generator", 3*time.Minute)
		t.Log("log-generator ready")

		// Inject forwarder sidecar with real image.
		cfg := sidecar.SidecarConfig{
			SessionID: "e2e-test",
			Target:    fmt.Sprintf("logtap-recv.%s:3100", testNS),
			Image:     forwarderImage,
		}
		w, err := k8s.DiscoverByName(ctx, nsClient, k8s.KindDeployment, "log-generator")
		if err != nil {
			t.Fatalf("DiscoverByName: %v", err)
		}
		result, err := sidecar.Inject(ctx, nsClient, w, cfg, false)
		if err != nil {
			t.Fatalf("Inject: %v", err)
		}
		if !result.Applied {
			t.Fatal("expected Applied=true")
		}
		t.Log("forwarder sidecar injected, waiting for rollout...")

		// Wait for deployment rollout with sidecar.
		waitForDeployment(t, ctx, client, testNS, "log-generator", 3*time.Minute)
		t.Log("rollout complete, polling receiver metrics...")

		// Poll receiver metrics via API proxy.
		metricsBody := pollReceiverMetrics(t, ctx, client, testNS, "logtap-recv", 3100, 3*time.Minute)
		val, ok := parseMetricValue(metricsBody, "logtap_logs_received_total")
		if !ok {
			t.Fatal("logtap_logs_received_total metric not found in receiver output")
		}
		if val <= 0 {
			t.Fatalf("logtap_logs_received_total = %v, want > 0", val)
		}
		t.Logf("logtap_logs_received_total = %v — full pipeline verified", val)
	})

	t.Run("RBACRestriction", func(t *testing.T) {
		// Create a ServiceAccount with no role bindings.
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "restricted",
				Namespace: testNS,
			},
		}
		_, err := client.CS.CoreV1().ServiceAccounts(testNS).Create(ctx, sa, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create SA: %v", err)
		}

		// Verify restricted SA cannot patch deployments.
		sar := &authv1.SubjectAccessReview{
			Spec: authv1.SubjectAccessReviewSpec{
				User: "system:serviceaccount:" + testNS + ":restricted",
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: testNS,
					Verb:      "patch",
					Group:     "apps",
					Resource:  "deployments",
				},
			},
		}
		resp, err := client.CS.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("SubjectAccessReview (patch): %v", err)
		}
		if resp.Status.Allowed {
			t.Error("restricted SA should not be allowed to patch deployments")
		}

		// Verify restricted SA cannot create pods.
		sar2 := &authv1.SubjectAccessReview{
			Spec: authv1.SubjectAccessReviewSpec{
				User: "system:serviceaccount:" + testNS + ":restricted",
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: testNS,
					Verb:      "create",
					Group:     "",
					Resource:  "pods",
				},
			},
		}
		resp2, err := client.CS.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar2, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("SubjectAccessReview (create): %v", err)
		}
		if resp2.Status.Allowed {
			t.Error("restricted SA should not be allowed to create pods")
		}

		t.Log("RBAC restriction verified: restricted SA denied patch + create")
	})
}

// pollReceiverMetrics polls the receiver's /metrics endpoint via Kubernetes API
// service proxy until logtap_logs_received_total > 0.
func pollReceiverMetrics(t *testing.T, ctx context.Context, c *k8s.Client, ns, svcName string, port int, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for logtap_logs_received_total > 0 from %s/%s", ns, svcName)
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for metrics")
		case <-tick.C:
			body, err := c.CS.CoreV1().RESTClient().Get().
				Namespace(ns).
				Resource("services").
				Name(fmt.Sprintf("%s:%d", svcName, port)).
				SubResource("proxy", "metrics").
				DoRaw(ctx)
			if err != nil {
				t.Logf("metrics probe: %v", err)
				continue
			}
			val, ok := parseMetricValue(string(body), "logtap_logs_received_total")
			if ok && val > 0 {
				return string(body)
			}
			t.Logf("logtap_logs_received_total = %v, waiting...", val)
		}
	}
}

// parseMetricValue extracts a float64 value from Prometheus text exposition format.
func parseMetricValue(body, metricName string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, metricName+" ") || strings.HasPrefix(line, metricName+"{") {
			// Handle both "metric_name 123" and "metric_name{labels} 123"
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				val, err := strconv.ParseFloat(parts[len(parts)-1], 64)
				if err == nil {
					return val, true
				}
			}
		}
	}
	return 0, false
}
