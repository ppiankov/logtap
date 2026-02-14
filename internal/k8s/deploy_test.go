package k8s

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func testReceiverSpec(ns string) ReceiverSpec {
	return ReceiverSpec{
		Image:     "ghcr.io/ppiankov/logtap:latest",
		Namespace: ns,
		PodName:   ReceiverName,
		SvcName:   ReceiverName,
		Port:      9000,
		Args:      []string{"recv", "--headless", "--listen", ":9000", "--dir", "/data"},
		Labels: map[string]string{
			LabelManagedBy: ManagedByValue,
			LabelName:      ReceiverName,
		},
	}
}

func TestDeployReceiver(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err != nil {
		t.Fatal(err)
	}

	if !res.CreatedNS {
		t.Error("CreatedNS = false, want true")
	}

	// verify namespace
	ns, err := cs.CoreV1().Namespaces().Get(context.Background(), "test-ns", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace not found: %v", err)
	}
	if ns.Labels[LabelManagedBy] != ManagedByValue {
		t.Errorf("namespace label = %q, want %q", ns.Labels[LabelManagedBy], ManagedByValue)
	}

	// verify pod
	pod, err := cs.CoreV1().Pods("test-ns").Get(context.Background(), ReceiverName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("pod not found: %v", err)
	}
	if pod.Spec.Containers[0].Image != "ghcr.io/ppiankov/logtap:latest" {
		t.Errorf("image = %q", pod.Spec.Containers[0].Image)
	}
	if pod.Labels[LabelName] != ReceiverName {
		t.Errorf("pod label = %q, want %q", pod.Labels[LabelName], ReceiverName)
	}

	// verify service
	svc, err := cs.CoreV1().Services("test-ns").Get(context.Background(), ReceiverName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("service not found: %v", err)
	}
	if svc.Spec.Ports[0].Port != 9000 {
		t.Errorf("service port = %d, want 9000", svc.Spec.Ports[0].Port)
	}
}

func TestDeployReceiver_ExistingNamespace(t *testing.T) {
	existingNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-ns"},
	}
	cs := fake.NewSimpleClientset(existingNS) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "existing-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("existing-ns"))
	if err != nil {
		t.Fatal(err)
	}

	if res.CreatedNS {
		t.Error("CreatedNS = true, want false (namespace already existed)")
	}
}

func TestDeleteReceiver(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err != nil {
		t.Fatal(err)
	}

	if err := DeleteReceiver(context.Background(), c, res); err != nil {
		t.Fatal(err)
	}

	// pod should be gone
	_, err = cs.CoreV1().Pods("test-ns").Get(context.Background(), ReceiverName, metav1.GetOptions{})
	if err == nil {
		t.Error("pod still exists after delete")
	}

	// service should be gone
	_, err = cs.CoreV1().Services("test-ns").Get(context.Background(), ReceiverName, metav1.GetOptions{})
	if err == nil {
		t.Error("service still exists after delete")
	}

	// namespace should be gone (we created it)
	_, err = cs.CoreV1().Namespaces().Get(context.Background(), "test-ns", metav1.GetOptions{})
	if err == nil {
		t.Error("namespace still exists after delete (CreatedNS was true)")
	}
}

func TestDeleteReceiver_PreservesNamespace(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err != nil {
		t.Fatal(err)
	}

	// pretend we did not create the namespace
	res.CreatedNS = false

	if err := DeleteReceiver(context.Background(), c, res); err != nil {
		t.Fatal(err)
	}

	// namespace should still exist
	_, err = cs.CoreV1().Namespaces().Get(context.Background(), "test-ns", metav1.GetOptions{})
	if err != nil {
		t.Error("namespace was deleted even though CreatedNS was false")
	}
}

func TestDeployReceiver_NamespaceError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("create", "namespaces", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected ns error")
	})
	c := NewClientFromInterface(cs, "test-ns")

	_, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err == nil {
		t.Fatal("expected error for namespace creation failure")
	}
}

func TestDeployReceiver_ServiceError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("create", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected svc error")
	})
	c := NewClientFromInterface(cs, "test-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err == nil {
		t.Fatal("expected error for service creation failure")
	}
	if res == nil {
		t.Fatal("resources should be returned even on error")
	}
}

func TestDeployReceiver_PodError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	cs.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected pod error")
	})
	c := NewClientFromInterface(cs, "test-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err == nil {
		t.Fatal("expected error for pod creation failure")
	}
	if res == nil {
		t.Fatal("resources should be returned even on error")
	}
}

func TestDeleteReceiver_PodDeleteError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	// deploy first
	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err != nil {
		t.Fatal(err)
	}

	// inject pod delete error
	cs.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected pod delete error")
	})

	err = DeleteReceiver(context.Background(), c, res)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "delete pod") {
		t.Errorf("err = %q, want 'delete pod'", err.Error())
	}
}

func TestDeleteReceiver_NamespaceDeleteError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err != nil {
		t.Fatal(err)
	}

	cs.PrependReactor("delete", "namespaces", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected ns delete error")
	})

	err = DeleteReceiver(context.Background(), c, res)
	if err == nil {
		t.Fatal("expected error for namespace delete failure")
	}
}

func TestDeleteReceiver_ServiceDeleteError(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	res, err := DeployReceiver(context.Background(), c, testReceiverSpec("test-ns"))
	if err != nil {
		t.Fatal(err)
	}

	// inject service delete error (pod delete succeeds)
	cs.PrependReactor("delete", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("injected service delete error")
	})

	err = DeleteReceiver(context.Background(), c, res)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "delete service") {
		t.Errorf("err = %q, want 'delete service'", err.Error())
	}
}

func TestWaitForPodReady_ContextCancel(t *testing.T) {
	cs := fake.NewSimpleClientset() //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := WaitForPodReady(ctx, c, "test-ns", "nonexistent", 10*time.Second)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWaitForPodReady_Timeout(t *testing.T) {
	// pod exists but never becomes ready
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: ReceiverName, Namespace: "test-ns"},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
		},
	}
	cs := fake.NewSimpleClientset(pod) //nolint:staticcheck // NewClientset requires generated apply configs
	c := NewClientFromInterface(cs, "test-ns")

	err := WaitForPodReady(context.Background(), c, "test-ns", ReceiverName, 2*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("err = %q, want timeout message", err.Error())
	}
}

func TestWaitForPodReady(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{ //nolint:staticcheck // NewClientset requires generated apply configs
		ObjectMeta: metav1.ObjectMeta{Name: ReceiverName, Namespace: "test-ns"},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	})
	c := NewClientFromInterface(cs, "test-ns")

	err := WaitForPodReady(context.Background(), c, "test-ns", ReceiverName, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForPodReady: %v", err)
	}
}
