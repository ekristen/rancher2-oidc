package storage

import (
	"context"
	"time"

	"github.com/ekristen/rancher-oidc-aggregator-ng/pkg/api"
)

// Storage defines the interface for storing and retrieving cluster identity data
type Storage interface {
	// Initialize sets up the storage backend
	Initialize(ctx context.Context) error

	// GetClusterIdentity retrieves the identity data for a specific cluster
	GetClusterIdentity(ctx context.Context, clusterID string) (*api.ClusterIdentity, error)

	// UpdateClusterIdentity stores or updates the identity data for a cluster
	UpdateClusterIdentity(ctx context.Context, identity *api.ClusterIdentity) error

	// StoreClusterIdentity is an alias for UpdateClusterIdentity for backwards compatibility
	StoreClusterIdentity(ctx context.Context, identity *api.ClusterIdentity) error

	// DeleteClusterIdentity removes a cluster's identity data
	DeleteClusterIdentity(ctx context.Context, clusterID string) error

	// ListClusters returns a list of all registered cluster IDs
	ListClusters(ctx context.Context) ([]string, error)

	// GetLastUpdated returns the last update timestamp for a cluster
	GetLastUpdated(ctx context.Context, clusterID string) (time.Time, error)

	// IsStale checks if the cached identity is older than maxAge
	IsStale(ctx context.Context, clusterID string, maxAge time.Duration) (bool, error)

	// Close cleans up any resources used by the storage backend
	Close() error
}

// StorageError represents a storage-related error
type StorageError struct {
	Message string
	Cause   error
}

func (e *StorageError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// ErrClusterNotFound is returned when a requested cluster doesn't exist
var ErrClusterNotFound = &StorageError{Message: "cluster not found"}

// ErrInvalidToken is returned when an invalid token is provided
// Deprecated: Token validation is no longer used
var ErrInvalidToken = &StorageError{Message: "invalid token"}
