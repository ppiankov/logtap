package k8s_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ppiankov/logtap/internal/k8s"
	"github.com/ppiankov/logtap/internal/sidecar"
)

// TestIntegration runs ordered subtests against a real Kind cluster.
// Requires LOGTAP_INTEGRATION=1 and a valid KUBECONFIG.
func TestIntegration(t *testing.T) {
	if os.Getenv("LOGTAP_INTEGRATION") == "" {
		t.Skip("set LOGTAP_INTEGRATION=1 to run integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	// Create a real client from KUBECONFIG.
	client, err := k8s.NewClient("")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Use a dedicated test namespace to avoid polluting default.
	testNS := fmt.Sprintf("logtap-inttest-%d", time.Now().UnixMilli()%100000)
	nsClient := k8s.NewClientFromInterface(client.CS, testNS)

	// Ensure namespace exists for all tests.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNS},
	}
	_, err = client.CS.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create test namespace %s: %v", testNS, err)
	}
	t.Cleanup(func() {
		_ = client.CS.CoreV1().Namespaces().Delete(context.Background(), testNS, metav1.DeleteOptions{})
	})

	t.Run("ClusterInfo", func(t *testing.T) {
		info, err := k8s.GetClusterInfo(ctx, nsClient)
		if err != nil {
			t.Fatalf("GetClusterInfo: %v", err)
		}
		if info.Version == "" {
			t.Error("version is empty")
		}
		if info.Namespace != testNS {
			t.Errorf("namespace = %q, want %q", info.Namespace, testNS)
		}
		t.Logf("cluster version: %s, namespace: %s", info.Version, info.Namespace)
	})

	// --- Receiver deploy/delete cycle ---

	var recvRes *k8s.ReceiverResources

	t.Run("DeployReceiver", func(t *testing.T) {
		recvNS := testNS + "-recv"
		labels := map[string]string{
			k8s.LabelManagedBy: k8s.ManagedByValue,
			k8s.LabelName:      k8s.ReceiverName,
		}
		spec := k8s.ReceiverSpec{
			Image:     "nginx:alpine",
			Namespace: recvNS,
			PodName:   "logtap-receiver",
			SvcName:   "logtap",
			Port:      80,
			Args:      nil, // nginx:alpine starts without args
			Labels:    labels,
		}

		recvRes, err = k8s.DeployReceiver(ctx, nsClient, spec)
		if err != nil {
			t.Fatalf("DeployReceiver: %v", err)
		}
		if !recvRes.CreatedNS {
			t.Error("expected namespace to be created")
		}

		// Verify pod was created (don't wait for Ready — nginx:alpine
		// doesn't serve /healthz and /readyz that the probe spec requires).
		_, err = client.CS.CoreV1().Pods(recvNS).Get(ctx, "logtap-receiver", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get pod: %v", err)
		}

		// Verify service exists.
		_, err = client.CS.CoreV1().Services(recvNS).Get(ctx, "logtap", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get service: %v", err)
		}
		t.Log("receiver pod ready, service created")
	})

	t.Run("DeleteReceiver", func(t *testing.T) {
		if recvRes == nil {
			t.Skip("DeployReceiver did not run")
		}
		recvClient := k8s.NewClientFromInterface(client.CS, recvRes.Namespace)
		err := k8s.DeleteReceiver(ctx, recvClient, recvRes)
		if err != nil {
			t.Fatalf("DeleteReceiver: %v", err)
		}
		t.Log("receiver cleaned up")
	})

	// --- Deployment patch cycle (sidecar inject/remove at k8s level) ---

	deployName := "inttest-nginx"

	t.Run("DeploymentPatch", func(t *testing.T) {
		// Create a minimal deployment.
		replicas := int32(1)
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName,
				Namespace: testNS,
				Labels:    map[string]string{"app": "inttest"},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "inttest"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "inttest"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:alpine",
							},
						},
					},
				},
			},
		}
		_, err := client.CS.AppsV1().Deployments(testNS).Create(ctx, deploy, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create deployment: %v", err)
		}

		// Wait for deployment to be available.
		waitForDeployment(t, ctx, client, testNS, deployName, 3*time.Minute)

		// Discover the workload.
		w, err := k8s.DiscoverByName(ctx, nsClient, k8s.KindDeployment, deployName)
		if err != nil {
			t.Fatalf("DiscoverByName: %v", err)
		}

		// Apply a patch (add sidecar container + annotations).
		ps := k8s.PatchSpec{
			Container: corev1.Container{
				Name:    "logtap-forwarder-lt-test",
				Image:   "busybox:1.36",
				Command: []string{"sleep", "3600"},
			},
			Annotations: map[string]string{
				sidecar.AnnotationTapped: "lt-test",
				sidecar.AnnotationTarget: "localhost:3100",
			},
		}
		diff, err := k8s.ApplyPatch(ctx, nsClient, w, ps, false)
		if err != nil {
			t.Fatalf("ApplyPatch: %v", err)
		}
		if diff == "" {
			t.Error("expected non-empty diff")
		}

		// Verify the container was added.
		updated, err := client.CS.AppsV1().Deployments(testNS).Get(ctx, deployName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get deployment: %v", err)
		}
		found := false
		for _, c := range updated.Spec.Template.Spec.Containers {
			if c.Name == "logtap-forwarder-lt-test" {
				found = true
				break
			}
		}
		if !found {
			t.Error("sidecar container not found after ApplyPatch")
		}
		ann := updated.Spec.Template.Annotations[sidecar.AnnotationTapped]
		if ann != "lt-test" {
			t.Errorf("tapped annotation = %q, want %q", ann, "lt-test")
		}
		t.Logf("patch applied, diff:\n%s", diff)
	})

	t.Run("DiscoverByName", func(t *testing.T) {
		w, err := k8s.DiscoverByName(ctx, nsClient, k8s.KindDeployment, deployName)
		if err != nil {
			t.Fatalf("DiscoverByName: %v", err)
		}
		if w.Name != deployName {
			t.Errorf("name = %q, want %q", w.Name, deployName)
		}
		if w.Kind != k8s.KindDeployment {
			t.Errorf("kind = %q, want %q", w.Kind, k8s.KindDeployment)
		}
		if w.Namespace != testNS {
			t.Errorf("namespace = %q, want %q", w.Namespace, testNS)
		}
	})

	t.Run("DiscoverBySelector", func(t *testing.T) {
		workloads, err := k8s.DiscoverBySelector(ctx, nsClient, "app=inttest")
		if err != nil {
			t.Fatalf("DiscoverBySelector: %v", err)
		}
		if len(workloads) != 1 {
			t.Fatalf("expected 1 workload, got %d", len(workloads))
		}
		if workloads[0].Name != deployName {
			t.Errorf("name = %q, want %q", workloads[0].Name, deployName)
		}
	})

	t.Run("DiscoverTapped", func(t *testing.T) {
		tapped, err := k8s.DiscoverTapped(ctx, nsClient, sidecar.AnnotationTapped)
		if err != nil {
			t.Fatalf("DiscoverTapped: %v", err)
		}
		if len(tapped) != 1 {
			t.Fatalf("expected 1 tapped workload, got %d", len(tapped))
		}
		if tapped[0].Name != deployName {
			t.Errorf("name = %q, want %q", tapped[0].Name, deployName)
		}
	})

	t.Run("FindOrphans", func(t *testing.T) {
		alwaysFalse := func(string) bool { return false }
		result, err := k8s.FindOrphans(ctx, nsClient,
			sidecar.AnnotationTapped, sidecar.AnnotationTarget,
			sidecar.ContainerPrefix, alwaysFalse)
		if err != nil {
			t.Fatalf("FindOrphans: %v", err)
		}
		if len(result.Sidecars) != 1 {
			t.Fatalf("expected 1 orphaned sidecar, got %d", len(result.Sidecars))
		}
		if result.Sidecars[0].TargetReachable {
			t.Error("expected target unreachable")
		}
		t.Logf("orphan detected: %s/%s", result.Sidecars[0].Workload.Kind, result.Sidecars[0].Workload.Name)
	})

	t.Run("RemovePatch", func(t *testing.T) {
		// Re-discover to get fresh Raw object.
		w, err := k8s.DiscoverByName(ctx, nsClient, k8s.KindDeployment, deployName)
		if err != nil {
			t.Fatalf("DiscoverByName: %v", err)
		}

		rs := k8s.RemovePatchSpec{
			ContainerNames:    []string{"logtap-forwarder-lt-test"},
			DeleteAnnotations: []string{sidecar.AnnotationTapped, sidecar.AnnotationTarget},
		}
		diff, err := k8s.RemovePatch(ctx, nsClient, w, rs, false)
		if err != nil {
			t.Fatalf("RemovePatch: %v", err)
		}
		if diff == "" {
			t.Error("expected non-empty diff")
		}

		// Verify container was removed.
		updated, err := client.CS.AppsV1().Deployments(testNS).Get(ctx, deployName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get deployment: %v", err)
		}
		for _, c := range updated.Spec.Template.Spec.Containers {
			if c.Name == "logtap-forwarder-lt-test" {
				t.Error("sidecar container still present after RemovePatch")
			}
		}
		if ann := updated.Spec.Template.Annotations[sidecar.AnnotationTapped]; ann != "" {
			t.Errorf("tapped annotation still present: %q", ann)
		}
		t.Log("patch removed")
	})

	t.Run("DiscoverTappedEmpty", func(t *testing.T) {
		tapped, err := k8s.DiscoverTapped(ctx, nsClient, sidecar.AnnotationTapped)
		if err != nil {
			t.Fatalf("DiscoverTapped: %v", err)
		}
		if len(tapped) != 0 {
			t.Errorf("expected 0 tapped workloads, got %d", len(tapped))
		}
	})

	// --- Prod namespace detection ---

	t.Run("ProdNamespace", func(t *testing.T) {
		prodNS := testNS + "-prod"
		prodNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   prodNS,
				Labels: map[string]string{"env": "prod"},
			},
		}
		_, err := client.CS.CoreV1().Namespaces().Create(ctx, prodNsObj, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create prod namespace: %v", err)
		}
		t.Cleanup(func() {
			_ = client.CS.CoreV1().Namespaces().Delete(context.Background(), prodNS, metav1.DeleteOptions{})
		})

		prodClient := k8s.NewClientFromInterface(client.CS, prodNS)
		isProd, err := k8s.IsProdNamespace(ctx, prodClient)
		if err != nil {
			t.Fatalf("IsProdNamespace: %v", err)
		}
		if !isProd {
			t.Error("expected namespace to be detected as prod")
		}

		// Non-prod namespace should return false.
		isProd, err = k8s.IsProdNamespace(ctx, nsClient)
		if err != nil {
			t.Fatalf("IsProdNamespace (non-prod): %v", err)
		}
		if isProd {
			t.Error("test namespace should not be detected as prod")
		}
	})

	// --- Quota check ---

	t.Run("QuotaCheck", func(t *testing.T) {
		// Use a separate namespace so the quota doesn't block other tests.
		quotaNS := testNS + "-quota"
		quotaNsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: quotaNS},
		}
		_, err := client.CS.CoreV1().Namespaces().Create(ctx, quotaNsObj, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create quota namespace: %v", err)
		}
		t.Cleanup(func() {
			_ = client.CS.CoreV1().Namespaces().Delete(context.Background(), quotaNS, metav1.DeleteOptions{})
		})
		quotaClient := k8s.NewClientFromInterface(client.CS, quotaNS)

		// Create a tight quota.
		quota := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tight-quota",
				Namespace: quotaNS,
			},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceRequestsMemory: resource.MustParse("32Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("50m"),
				},
			},
		}
		_, err = client.CS.CoreV1().ResourceQuotas(quotaNS).Create(ctx, quota, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create quota: %v", err)
		}

		// Wait for the quota controller to populate Status.Hard
		// (newly created quotas have empty Status until the controller syncs).
		waitForQuotaSync(t, ctx, client, quotaNS, "tight-quota", 30*time.Second)

		// CheckResources with 3 replicas × 16Mi should exceed 32Mi quota.
		warnings, err := k8s.CheckResources(ctx, quotaClient, 3, "16Mi", "25m")
		if err != nil {
			t.Fatalf("CheckResources: %v", err)
		}
		foundMemWarn := false
		for _, w := range warnings {
			if w.Check == "quota" {
				foundMemWarn = true
				t.Logf("quota warning: %s", w.Message)
			}
		}
		if !foundMemWarn {
			t.Error("expected memory quota warning for 3 × 16Mi > 32Mi")
		}
	})

	// --- Node capacity (smoke test, Kind node should be healthy) ---

	t.Run("NodeCapacity", func(t *testing.T) {
		// Just verify it doesn't error. Kind node should have no pressure.
		warnings, err := k8s.CheckResources(ctx, nsClient, 1, "16Mi", "25m")
		if err != nil {
			t.Fatalf("CheckResources: %v", err)
		}
		for _, w := range warnings {
			if w.Check == "capacity" {
				t.Logf("node warning (unexpected in Kind): %s", w.Message)
			}
		}
	})

	// --- RBAC check (Kind gives cluster-admin, so all should be allowed) ---

	t.Run("RBAC", func(t *testing.T) {
		checks := []k8s.RBACCheck{
			{Resource: "deployments", Verb: "get", Group: "apps"},
			{Resource: "deployments", Verb: "update", Group: "apps"},
			{Resource: "pods", Verb: "create", Group: ""},
			{Resource: "pods", Verb: "list", Group: ""},
		}
		results, err := k8s.CheckRBAC(ctx, nsClient, checks)
		if err != nil {
			t.Fatalf("CheckRBAC: %v", err)
		}
		for _, r := range results {
			if !r.Allowed {
				t.Errorf("RBAC check %s/%s %s: not allowed (expected allowed in Kind)",
					r.Check.Group, r.Check.Resource, r.Check.Verb)
			}
		}
	})

	// --- Full sidecar.Inject → sidecar.Remove cycle ---

	t.Run("SidecarInjectRemove", func(t *testing.T) {
		// Create a fresh deployment for this test.
		deployName2 := "inttest-sidecar"
		replicas := int32(1)
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName2,
				Namespace: testNS,
				Labels:    map[string]string{"app": "inttest-sc"},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "inttest-sc"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "inttest-sc"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:alpine",
							},
						},
					},
				},
			},
		}
		_, err := client.CS.AppsV1().Deployments(testNS).Create(ctx, deploy, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create deployment: %v", err)
		}
		waitForDeployment(t, ctx, client, testNS, deployName2, 3*time.Minute)

		// Discover workload.
		w, err := k8s.DiscoverByName(ctx, nsClient, k8s.KindDeployment, deployName2)
		if err != nil {
			t.Fatalf("DiscoverByName: %v", err)
		}

		// Inject sidecar.
		sessionID := "lt-inttest"
		cfg := sidecar.SidecarConfig{
			SessionID: sessionID,
			Target:    "localhost:3100",
			Image:     "busybox:1.36",
		}
		result, err := sidecar.Inject(ctx, nsClient, w, cfg, false)
		if err != nil {
			t.Fatalf("Inject: %v", err)
		}
		if !result.Applied {
			t.Error("expected Applied=true")
		}
		if result.SessionID != sessionID {
			t.Errorf("session = %q, want %q", result.SessionID, sessionID)
		}

		// Verify sidecar exists in deployment.
		updated, err := client.CS.AppsV1().Deployments(testNS).Get(ctx, deployName2, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get deployment: %v", err)
		}
		sidecarFound := false
		for _, c := range updated.Spec.Template.Spec.Containers {
			if c.Name == sidecar.ContainerPrefix+sessionID {
				sidecarFound = true
				break
			}
		}
		if !sidecarFound {
			t.Error("sidecar container not found after Inject")
		}

		// Remove sidecar with retry — the deployment controller may update
		// the resource after Inject, causing an optimistic concurrency conflict.
		var removeResult *sidecar.RemoveResult
		for attempt := 0; attempt < 5; attempt++ {
			w, err = k8s.DiscoverByName(ctx, nsClient, k8s.KindDeployment, deployName2)
			if err != nil {
				t.Fatalf("DiscoverByName: %v", err)
			}
			removeResult, err = sidecar.Remove(ctx, nsClient, w, sessionID, false)
			if err == nil {
				break
			}
			if !strings.Contains(err.Error(), "the object has been modified") {
				t.Fatalf("Remove: %v", err)
			}
			t.Logf("retry %d: conflict on Remove, re-discovering...", attempt+1)
			time.Sleep(time.Second)
		}
		if err != nil {
			t.Fatalf("Remove: exhausted retries: %v", err)
		}
		if !removeResult.Applied {
			t.Error("expected Applied=true")
		}

		// Verify sidecar removed.
		updated, err = client.CS.AppsV1().Deployments(testNS).Get(ctx, deployName2, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get deployment: %v", err)
		}
		for _, c := range updated.Spec.Template.Spec.Containers {
			if c.Name == sidecar.ContainerPrefix+sessionID {
				t.Error("sidecar container still present after Remove")
			}
		}
		if ann := updated.Spec.Template.Annotations[sidecar.AnnotationTapped]; ann != "" {
			t.Errorf("tapped annotation still present: %q", ann)
		}
		t.Log("sidecar inject/remove cycle complete")
	})
}

// waitForQuotaSync polls until a ResourceQuota's Status.Hard is populated.
func waitForQuotaSync(t *testing.T, ctx context.Context, c *k8s.Client, ns, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for quota %s/%s status sync", ns, name)
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for quota %s/%s", ns, name)
		case <-tick.C:
			q, err := c.CS.CoreV1().ResourceQuotas(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			if len(q.Status.Hard) > 0 {
				return
			}
		}
	}
}

// waitForDeployment polls until the deployment has at least one available replica.
func waitForDeployment(t *testing.T, ctx context.Context, c *k8s.Client, ns, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for deployment %s/%s", ns, name)
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for deployment %s/%s", ns, name)
		case <-tick.C:
			d, err := c.CS.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			if d.Status.AvailableReplicas > 0 {
				return
			}
		}
	}
}
