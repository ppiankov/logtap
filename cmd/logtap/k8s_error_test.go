package main

import "testing"

func TestRunStatus_NoKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", "/nonexistent")

	err := runStatus("default", false)
	if err == nil {
		t.Fatal("expected error without kubeconfig")
	}
	if !containsString(err.Error(), "connect to cluster") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCheck_NoKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", "/nonexistent")

	err := runCheck("default", false)
	if err == nil {
		t.Fatal("expected error without kubeconfig")
	}
	if !containsString(err.Error(), "connect to cluster") {
		t.Fatalf("unexpected error: %v", err)
	}
}
