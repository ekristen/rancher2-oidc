package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/ekristen/rancher2-oidc/pkg/api"
)

const (
	secretLabel     = "oidc-aggregator.rancher.io/managed"
	clusterIDLabel  = "oidc-aggregator.rancher.io/cluster-id"
	secretDataKey   = "identity"
	secretNamespace = "cattle-system"
)

// KubernetesStorage implements the Storage interface using Kubernetes Secrets
type KubernetesStorage struct {
	client kubernetes.Interface
}

// NewKubernetesStorage creates a new KubernetesStorage instance
func NewKubernetesStorage(client kubernetes.Interface) *KubernetesStorage {
	return &KubernetesStorage{
		client: client,
	}
}

// Initialize ensures the storage backend is ready
func (s *KubernetesStorage) Initialize(ctx context.Context) error {
	// No initialization needed for Kubernetes storage
	return nil
}

// GetClusterIdentity retrieves identity data from a Secret
func (s *KubernetesStorage) GetClusterIdentity(ctx context.Context, clusterID string) (*api.ClusterIdentity, error) {
	secretName := fmt.Sprintf("oidc-%s", clusterID)
	secret, err := s.client.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrClusterNotFound
		}
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	data, ok := secret.Data[secretDataKey]
	if !ok {
		return nil, fmt.Errorf("secret data not found")
	}

	var identity api.ClusterIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return nil, fmt.Errorf("failed to unmarshal identity data: %w", err)
	}

	return &identity, nil
}

// UpdateClusterIdentity stores identity data in a Secret
func (s *KubernetesStorage) UpdateClusterIdentity(ctx context.Context, identity *api.ClusterIdentity) error {
	secretName := fmt.Sprintf("oidc-%s", identity.ClusterID)
	data, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("failed to marshal identity data: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
			Labels: map[string]string{
				secretLabel:    "true",
				clusterIDLabel: identity.ClusterID,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			secretDataKey: data,
		},
	}

	_, err = s.client.CoreV1().Secrets(secretNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Update existing secret
			existing, err := s.client.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get existing secret: %w", err)
			}
			existing.Data[secretDataKey] = data
			_, err = s.client.CoreV1().Secrets(secretNamespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create secret: %w", err)
	}

	return nil
}

// StoreClusterIdentity is an alias for UpdateClusterIdentity
func (s *KubernetesStorage) StoreClusterIdentity(ctx context.Context, identity *api.ClusterIdentity) error {
	return s.UpdateClusterIdentity(ctx, identity)
}

// DeleteClusterIdentity removes a cluster's identity Secret
func (s *KubernetesStorage) DeleteClusterIdentity(ctx context.Context, clusterID string) error {
	secretName := fmt.Sprintf("oidc-%s", clusterID)
	err := s.client.CoreV1().Secrets(secretNamespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ErrClusterNotFound
		}
		return fmt.Errorf("failed to delete secret: %w", err)
	}
	return nil
}

// ListClusters returns a list of all registered cluster IDs
func (s *KubernetesStorage) ListClusters(ctx context.Context) ([]string, error) {
	selector := fmt.Sprintf("%s=true", secretLabel)
	secrets, err := s.client.CoreV1().Secrets(secretNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	clusters := make([]string, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		if clusterID, ok := secret.Labels[clusterIDLabel]; ok {
			clusters = append(clusters, clusterID)
		}
	}

	return clusters, nil
}

// GetLastUpdated returns the last update timestamp for a cluster
func (s *KubernetesStorage) GetLastUpdated(ctx context.Context, clusterID string) (time.Time, error) {
	identity, err := s.GetClusterIdentity(ctx, clusterID)
	if err != nil {
		return time.Time{}, err
	}
	return identity.UpdatedAt, nil
}

// IsStale checks if the cached identity is older than maxAge
func (s *KubernetesStorage) IsStale(ctx context.Context, clusterID string, maxAge time.Duration) (bool, error) {
	updatedAt, err := s.GetLastUpdated(ctx, clusterID)
	if err != nil {
		if err == ErrClusterNotFound {
			// No cache exists, consider it stale
			return true, nil
		}
		return false, err
	}

	return time.Since(updatedAt) > maxAge, nil
}

// Close performs any necessary cleanup
func (s *KubernetesStorage) Close() error {
	return nil
}
