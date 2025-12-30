package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/rest"

	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/api"
	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/rancher"
)

// Fetcher retrieves OIDC configuration and JWKS from downstream clusters
type Fetcher struct {
	rancherClient *rancher.Client
	logger        *zap.Logger
}

// NewFetcher creates a new Fetcher instance
func NewFetcher(rancherClient *rancher.Client, logger *zap.Logger) *Fetcher {
	return &Fetcher{
		rancherClient: rancherClient,
		logger:        logger,
	}
}

// FetchClusterIdentity retrieves the OIDC configuration and JWKS from a downstream cluster
func (f *Fetcher) FetchClusterIdentity(ctx context.Context, clusterID string) (*api.ClusterIdentity, error) {
	// Get client for the downstream cluster
	_, config, err := f.rancherClient.GetClusterClient(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster client: %w", err)
	}

	// Fetch OIDC configuration
	oidcConfig, err := f.fetchOIDCConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC configuration: %w", err)
	}

	// Fetch JWKS
	jwks, err := f.fetchJWKS(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	return &api.ClusterIdentity{
		ClusterID:  clusterID,
		OIDCConfig: *oidcConfig,
		JWKS:       *jwks,
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

// fetchOIDCConfig fetches the OIDC discovery document from a cluster
func (f *Fetcher) fetchOIDCConfig(ctx context.Context, config *rest.Config) (*api.OIDCConfiguration, error) {
	// Create a REST client for raw API calls
	restClient, err := f.createRESTClient(config)
	if err != nil {
		return nil, err
	}

	// Fetch OIDC configuration from /.well-known/openid-configuration
	data, err := restClient.Get().AbsPath("/.well-known/openid-configuration").DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC configuration: %w", err)
	}

	var oidcConfig api.OIDCConfiguration
	if err := json.Unmarshal(data, &oidcConfig); err != nil {
		return nil, fmt.Errorf("failed to parse OIDC configuration: %w", err)
	}

	return &oidcConfig, nil
}

// fetchJWKS fetches the JWKS from a cluster
func (f *Fetcher) fetchJWKS(ctx context.Context, config *rest.Config) (*api.JWKS, error) {
	// Create a REST client for raw API calls
	restClient, err := f.createRESTClient(config)
	if err != nil {
		return nil, err
	}

	// Fetch JWKS from /openid/v1/jwks
	data, err := restClient.Get().AbsPath("/openid/v1/jwks").DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	var jwks api.JWKS
	if err := json.Unmarshal(data, &jwks); err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}

	return &jwks, nil
}

// createRESTClient creates a REST client configured for raw API calls
func (f *Fetcher) createRESTClient(config *rest.Config) (*rest.RESTClient, error) {
	// Copy the config to avoid modifying the original
	configCopy := rest.CopyConfig(config)

	// Clear settings that might interfere with raw API calls
	configCopy.APIPath = ""
	configCopy.GroupVersion = nil
	configCopy.NegotiatedSerializer = nil

	restClient, err := rest.UnversionedRESTClientFor(configCopy)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	return restClient, nil
}

// ListAvailableClusters returns a list of all cluster IDs that can be fetched
func (f *Fetcher) ListAvailableClusters(ctx context.Context) ([]string, error) {
	clusters, err := f.rancherClient.ListClusters(ctx)
	if err != nil {
		return nil, err
	}

	clusterIDs := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.ClusterName != "" && cluster.ClientSecretName != "" {
			clusterIDs = append(clusterIDs, cluster.ClusterName)
		}
	}

	return clusterIDs, nil
}
