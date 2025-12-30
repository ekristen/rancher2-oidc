package kube

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetConfig returns a Kubernetes REST config, trying in-cluster first,
// then falling back to KUBECONFIG or default kubeconfig location
func GetConfig(kubeconfig string) (*rest.Config, error) {
	// If explicit kubeconfig path provided, use it
	if kubeconfig != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig %s: %w", kubeconfig, err)
		}
		return config, nil
	}

	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to KUBECONFIG environment variable
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfigEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from KUBECONFIG: %w", err)
		}
		return config, nil
	}

	// Fall back to default kubeconfig location (~/.kube/config)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	defaultKubeconfig := filepath.Join(homeDir, ".kube", "config")
	if _, err := os.Stat(defaultKubeconfig); err == nil {
		config, err := clientcmd.BuildConfigFromFlags("", defaultKubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from default kubeconfig: %w", err)
		}
		return config, nil
	}

	return nil, fmt.Errorf("no valid kubernetes configuration found (tried in-cluster, KUBECONFIG env, and ~/.kube/config)")
}
