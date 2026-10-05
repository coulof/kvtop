package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client wraps Kubernetes clientset, dynamic client, and REST configuration.
type Client struct {
	Clientset  kubernetes.Interface
	Dynamic    dynamic.Interface
	RestConfig *rest.Config
}

// NewClient creates a Kubernetes client resolving kubeconfig in standard priority order:
// 1. Explicit path parameter (e.g. from --kubeconfig CLI flag)
// 2. KUBECONFIG environment variable
// 3. ~/.kube/config default location
// 4. In-cluster configuration fallback
func NewClient(kubeconfigPath string) (*Client, error) {
	config, err := loadRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Optimize client for parallel metric scraping across nodes
	config.QPS = 50.0
	config.Burst = 100
	config.Timeout = 5 * time.Second

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &Client{
		Clientset:  cs,
		Dynamic:    dyn,
		RestConfig: config,
	}, nil
}

func loadRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", envKubeconfig)
	}

	if home := homedir.HomeDir(); home != "" {
		defaultPath := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(defaultPath); err == nil {
			return clientcmd.BuildConfigFromFlags("", defaultPath)
		}
	}

	return rest.InClusterConfig()
}
