package k8s

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps a Kubernetes clientset and namespace.
type Client struct {
	CS kubernetes.Interface
	NS string
}

// NewClient creates a Client from the default kubeconfig.
func NewClient(namespace string) (*Client, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if namespace != "" {
		overrides.Context.Namespace = namespace
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	ns := namespace
	if ns == "" {
		var err error
		ns, _, err = config.Namespace()
		if err != nil {
			return nil, fmt.Errorf("resolve namespace: %w", err)
		}
	}

	restConfig, err := config.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}

	cs, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	return &Client{CS: cs, NS: ns}, nil
}

// NewClientFromInterface creates a Client from an existing clientset (for testing).
func NewClientFromInterface(cs kubernetes.Interface, ns string) *Client {
	return &Client{CS: cs, NS: ns}
}
