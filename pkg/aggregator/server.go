package aggregator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/api"
	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/fetcher"
	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/kube"
	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/rancher"
	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/storage"
)

// LocalClusterID is the special cluster ID for the local/management cluster
const LocalClusterID = "local"

// Server represents the OIDC aggregator server
type Server struct {
	router           *chi.Mux
	baseURL          string
	storage          storage.Storage
	logger           *zap.Logger
	localRestClient  rest.Interface // REST client for local cluster (in-cluster)
	rancherClient    *rancher.Client
	fetcher          *fetcher.Fetcher
	cacheTTL         time.Duration
	localInitialized bool
}

// Options contains configuration options for the server
type Options struct {
	BaseURL    string
	Logger     *zap.Logger
	ClusterID  string // Deprecated: local cluster is always "local"
	CacheTTL   time.Duration
	Kubeconfig string // Path to kubeconfig file (optional, uses in-cluster if empty)
}

// NewServer creates a new OIDC aggregator server instance
func NewServer(opts Options) (*Server, error) {
	// Get Kubernetes config for Rancher API access (supports both in-cluster and KUBECONFIG)
	config, err := kube.GetConfig(opts.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	// Create the clientset for Rancher API
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create kubernetes storage
	store := storage.NewKubernetesStorage(clientset)

	// Create rancher client for accessing downstream clusters
	rancherClient, err := rancher.NewClient(config, opts.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create rancher client: %w", err)
	}

	// Create fetcher for retrieving OIDC data from downstream clusters
	clusterFetcher := fetcher.NewFetcher(rancherClient, opts.Logger)

	// Try to create a local cluster REST client using in-cluster config
	// This is used for fetching OIDC/JWKS from the local cluster where the aggregator runs
	var localRestClient rest.Interface
	inClusterConfig, err := rest.InClusterConfig()
	if err != nil {
		opts.Logger.Warn("failed to get in-cluster config for local cluster, local cluster OIDC will not be available",
			zap.Error(err))
	} else {
		localClientset, err := kubernetes.NewForConfig(inClusterConfig)
		if err != nil {
			opts.Logger.Warn("failed to create local cluster clientset, local cluster OIDC will not be available",
				zap.Error(err))
		} else {
			localRestClient = localClientset.RESTClient()
		}
	}

	router := chi.NewRouter()

	// Default cache TTL to 15 minutes if not specified
	cacheTTL := opts.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 15 * time.Minute
	}

	s := &Server{
		router:          router,
		storage:         store,
		logger:          opts.Logger,
		baseURL:         opts.BaseURL,
		localRestClient: localRestClient,
		rancherClient:   rancherClient,
		fetcher:         clusterFetcher,
		cacheTTL:        cacheTTL,
	}

	s.setupRoutes()
	return s, nil
}

// setupRoutes configures all HTTP routes for the server
func (s *Server) setupRoutes() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))

	// Health check endpoint
	s.router.Get("/healthz", s.handleHealthCheck)

	// Public OIDC discovery endpoints under /oidc prefix
	s.router.Route("/oidc/{cluster_id}", func(r chi.Router) {
		r.Get("/.well-known/openid-configuration", s.handleOIDCDiscovery)
		r.Get("/jwks", s.handleJWKS)
	})
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// fetchLocalClusterIdentity fetches OIDC/JWKS from the local cluster using in-cluster config
func (s *Server) fetchLocalClusterIdentity(ctx context.Context) (*api.ClusterIdentity, error) {
	if s.localRestClient == nil {
		return nil, fmt.Errorf("local cluster REST client not available (not running in-cluster)")
	}

	// Fetch OIDC configuration
	oidcConfigRaw, err := s.localRestClient.Get().AbsPath("/.well-known/openid-configuration").DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch local OIDC configuration: %w", err)
	}

	// Fetch JWKS
	jwksRaw, err := s.localRestClient.Get().AbsPath("/openid/v1/jwks").DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch local JWKS: %w", err)
	}

	// Parse OIDC config
	var oidcConfig api.OIDCConfiguration
	if err := json.Unmarshal(oidcConfigRaw, &oidcConfig); err != nil {
		return nil, fmt.Errorf("invalid local OIDC configuration: %w", err)
	}

	// Parse JWKS
	var jwks api.JWKS
	if err := json.Unmarshal(jwksRaw, &jwks); err != nil {
		return nil, fmt.Errorf("invalid local JWKS: %w", err)
	}

	return &api.ClusterIdentity{
		ClusterID:  LocalClusterID,
		OIDCConfig: oidcConfig,
		JWKS:       jwks,
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

// getClusterIdentity retrieves cluster identity from cache or fetches from downstream cluster
func (s *Server) getClusterIdentity(ctx context.Context, clusterID string) (*api.ClusterIdentity, error) {
	// Check if we have a cached version
	identity, err := s.storage.GetClusterIdentity(ctx, clusterID)
	cacheExists := err == nil && identity != nil

	// Check if cache is stale
	isStale, err := s.storage.IsStale(ctx, clusterID, s.cacheTTL)
	if err != nil && err != storage.ErrClusterNotFound {
		s.logger.Warn("failed to check cache staleness",
			zap.String("cluster_id", clusterID),
			zap.Error(err))
	}

	// If cache exists and is fresh, return it
	if cacheExists && !isStale {
		s.logger.Debug("returning cached cluster identity",
			zap.String("cluster_id", clusterID),
			zap.Time("updated_at", identity.UpdatedAt))
		return identity, nil
	}

	// Try to fetch fresh data
	s.logger.Debug("fetching cluster identity",
		zap.String("cluster_id", clusterID),
		zap.Bool("cache_exists", cacheExists),
		zap.Bool("is_stale", isStale))

	var freshIdentity *api.ClusterIdentity
	var fetchErr error

	// Handle "local" cluster specially - use in-cluster config
	if clusterID == LocalClusterID {
		freshIdentity, fetchErr = s.fetchLocalClusterIdentity(ctx)
	} else {
		// Fetch from downstream cluster via Rancher
		freshIdentity, fetchErr = s.fetcher.FetchClusterIdentity(ctx, clusterID)
	}

	if fetchErr != nil {
		s.logger.Warn("failed to fetch cluster identity",
			zap.String("cluster_id", clusterID),
			zap.Error(fetchErr))

		// If we have stale cache, return it as fallback
		if cacheExists {
			s.logger.Info("returning stale cached identity as fallback",
				zap.String("cluster_id", clusterID),
				zap.Time("updated_at", identity.UpdatedAt))
			return identity, nil
		}

		// No cache and fetch failed
		return nil, fmt.Errorf("failed to get cluster identity: %w", fetchErr)
	}

	// Cache the fresh data
	if err := s.storage.UpdateClusterIdentity(ctx, freshIdentity); err != nil {
		s.logger.Warn("failed to cache cluster identity",
			zap.String("cluster_id", clusterID),
			zap.Error(err))
		// Continue anyway, we have the fresh data
	}

	return freshIdentity, nil
}

// handleOIDCDiscovery serves the OIDC discovery document
func (s *Server) handleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "cluster_id")

	identity, err := s.getClusterIdentity(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("failed to get cluster identity", zap.Error(err))
		s.sendError(w, http.StatusNotFound, "cluster not found")
		return
	}

	// Ensure the issuer and jwks_uri use the correct base URL
	identity.OIDCConfig.Issuer = fmt.Sprintf("%s/oidc/%s", s.baseURL, clusterID)
	identity.OIDCConfig.JWKSURI = fmt.Sprintf("%s/oidc/%s/jwks", s.baseURL, clusterID)

	s.sendJSON(w, http.StatusOK, identity.OIDCConfig)
}

// handleJWKS serves the JWKS document
func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "cluster_id")

	identity, err := s.getClusterIdentity(r.Context(), clusterID)
	if err != nil {
		s.logger.Error("failed to get cluster identity", zap.Error(err))
		s.sendError(w, http.StatusNotFound, "cluster not found")
		return
	}

	s.sendJSON(w, http.StatusOK, identity.JWKS)
}

// handleHealthCheck responds to health check requests
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	// Check if storage is accessible
	clusters, err := s.storage.ListClusters(r.Context())
	if err != nil {
		s.logger.Error("health check failed", zap.Error(err))
		s.sendJSON(w, http.StatusServiceUnavailable, api.APIResponse{
			Success: false,
			Error:   "storage check failed",
		})
		return
	}

	// Try to list available clusters from Rancher
	availableClusters, err := s.fetcher.ListAvailableClusters(r.Context())
	if err != nil {
		s.logger.Warn("failed to list available clusters from rancher", zap.Error(err))
	}

	s.sendJSON(w, http.StatusOK, api.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"status":              "healthy",
			"cached_clusters":     len(clusters),
			"available_clusters":  len(availableClusters),
			"local_cluster_ready": s.localRestClient != nil,
			"cache_ttl":           s.cacheTTL.String(),
			"timestamp":           time.Now().UTC(),
			"storage_status":      "ok",
		},
	})
}

// sendJSON sends a JSON response with the given status code and data
func (s *Server) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode JSON response", zap.Error(err))
	}
}

// sendError sends an error response with the given status code and message
func (s *Server) sendError(w http.ResponseWriter, status int, message string) {
	s.sendJSON(w, status, api.APIResponse{
		Success: false,
		Error:   message,
	})
}
