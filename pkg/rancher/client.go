package rancher

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ClusterInfo contains information about a Rancher-managed cluster
type ClusterInfo struct {
	// Name is the cluster resource name (e.g., "odin-rke2")
	Name string
	// Namespace is the namespace where the cluster resource lives
	Namespace string
	// ClusterName is the internal cluster ID (e.g., "c-m-sdw5jvlh")
	ClusterName string
	// ClientSecretName is the name of the secret containing the kubeconfig
	ClientSecretName string
}

// Client provides access to Rancher cluster resources
type Client struct {
	dynamicClient dynamic.Interface
	clientset     kubernetes.Interface
	logger        *zap.Logger
}

// clusterGVR is the GroupVersionResource for provisioning.cattle.io/v1 Cluster
var clusterGVR = schema.GroupVersionResource{
	Group:    "provisioning.cattle.io",
	Version:  "v1",
	Resource: "clusters",
}

// NewClient creates a new Rancher client
func NewClient(config *rest.Config, logger *zap.Logger) (*Client, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Client{
		dynamicClient: dynamicClient,
		clientset:     clientset,
		logger:        logger,
	}, nil
}

// ListClusters returns all Rancher-managed clusters across all namespaces
func (c *Client) ListClusters(ctx context.Context) ([]ClusterInfo, error) {
	list, err := c.dynamicClient.Resource(clusterGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}

	clusters := make([]ClusterInfo, 0, len(list.Items))
	for _, item := range list.Items {
		info, err := c.parseClusterInfo(&item)
		if err != nil {
			c.logger.Warn("failed to parse cluster info",
				zap.String("name", item.GetName()),
				zap.String("namespace", item.GetNamespace()),
				zap.Error(err))
			continue
		}
		clusters = append(clusters, *info)
	}

	return clusters, nil
}

// GetCluster retrieves a specific cluster by its resource name (e.g., "cluster-rke2-dev-310a")
// or by its internal cluster ID (e.g., "c-m-sdw5jvlh" from status.clusterName).
func (c *Client) GetCluster(ctx context.Context, clusterName string) (*ClusterInfo, error) {
	clusters, err := c.ListClusters(ctx)
	if err != nil {
		return nil, err
	}

	for _, cluster := range clusters {
		// Match by resource name (human-readable) or internal cluster ID
		if cluster.Name == clusterName || cluster.ClusterName == clusterName {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("cluster not found: %s", clusterName)
}

// GetClusterClient returns a Kubernetes clientset for a downstream cluster
func (c *Client) GetClusterClient(ctx context.Context, clusterName string) (kubernetes.Interface, *rest.Config, error) {
	cluster, err := c.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, nil, err
	}

	if cluster.ClientSecretName == "" {
		return nil, nil, fmt.Errorf("cluster %s has no clientSecretName", clusterName)
	}

	// Get the kubeconfig secret
	secret, err := c.clientset.CoreV1().Secrets(cluster.Namespace).Get(ctx, cluster.ClientSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get kubeconfig secret %s/%s: %w", cluster.Namespace, cluster.ClientSecretName, err)
	}

	// Extract kubeconfig from secret - the "value" key contains the full kubeconfig
	kubeconfigData, ok := secret.Data["value"]
	if !ok {
		return nil, nil, fmt.Errorf("kubeconfig secret %s/%s missing 'value' key", cluster.Namespace, cluster.ClientSecretName)
	}

	// Parse the kubeconfig
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Create clientset for the downstream cluster
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create downstream cluster client: %w", err)
	}

	return clientset, config, nil
}

// GetClusterRESTClient returns a REST client for a downstream cluster (for raw API calls)
func (c *Client) GetClusterRESTClient(ctx context.Context, clusterName string) (*rest.RESTClient, error) {
	_, config, err := c.GetClusterClient(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	// Configure the REST client for raw API calls
	config.ContentConfig.GroupVersion = &corev1.SchemeGroupVersion
	config.ContentConfig.NegotiatedSerializer = nil
	config.APIPath = ""

	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	return restClient, nil
}

// parseClusterInfo extracts ClusterInfo from an unstructured Cluster resource
func (c *Client) parseClusterInfo(obj *unstructured.Unstructured) (*ClusterInfo, error) {
	status, found, err := unstructured.NestedMap(obj.Object, "status")
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("status not found")
	}

	clusterName, _, _ := unstructured.NestedString(status, "clusterName")
	clientSecretName, _, _ := unstructured.NestedString(status, "clientSecretName")

	return &ClusterInfo{
		Name:             obj.GetName(),
		Namespace:        obj.GetNamespace(),
		ClusterName:      clusterName,
		ClientSecretName: clientSecretName,
	}, nil
}
