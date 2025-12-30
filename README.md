# Rancher OIDC Aggregator

A centralized service that aggregates OpenID Connect (OIDC) discovery documents and JSON Web Key Sets (JWKS) from Rancher-managed Kubernetes clusters.

## Overview

The OIDC Aggregator runs on your Rancher management cluster and provides unified OIDC endpoints for all managed clusters. It pulls OIDC/JWKS data directly from downstream clusters using Rancher's cluster management API.

**Key Features:**

- Unified OIDC discovery endpoints per cluster (`/oidc/{cluster_id}/.well-known/openid-configuration`)
- JWKS endpoints per cluster (`/oidc/{cluster_id}/jwks`)
- Automatic discovery of Rancher-managed clusters via `provisioning.cattle.io/v1` API
- Caching with configurable TTL (default: 15 minutes)
- Fallback to stale cache when downstream clusters are unreachable
- Special `local` cluster support for the management cluster itself

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Rancher Management Cluster                │
├─────────────────────────────────────────────────────────────┤
│  OIDC Aggregator                                             │
│  ├─ GET /oidc/{cluster_id}/.well-known/openid-configuration │
│  ├─ GET /oidc/{cluster_id}/jwks                             │
│  └─ GET /healthz                                            │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Cache (Kubernetes Secrets in cattle-system)            │ │
│  └────────────────────────────────────────────────────────┘ │
│                          │                                   │
│           ┌──────────────┴──────────────┐                   │
│           ▼                              ▼                   │
│  ┌─────────────────┐          ┌─────────────────────────┐   │
│  │ Local Cluster   │          │ Rancher Cluster API     │   │
│  │ (in-cluster)    │          │ provisioning.cattle.io  │   │
│  └─────────────────┘          └───────────┬─────────────┘   │
└───────────────────────────────────────────┼─────────────────┘
                                            │
                    ┌───────────────────────┼───────────────────────┐
                    ▼                       ▼                       ▼
           ┌──────────────┐        ┌──────────────┐        ┌──────────────┐
           │ Downstream   │        │ Downstream   │        │ Downstream   │
           │ Cluster A    │        │ Cluster B    │        │ Cluster C    │
           └──────────────┘        └──────────────┘        └──────────────┘
```

## Installation

### Prerequisites

- Kubernetes cluster with Rancher installed
- Helm 3.x
- Access to downstream cluster kubeconfigs via Rancher

### Helm Installation

```bash
# Install the chart
helm install rancher2-oidc oci://ghcr.io/ekristen/charts/rancher2-oidc \
  --namespace cattle-system \
  --set baseURL=https://your-rancher-domain.com \
  --set ingress.enabled=true \
  --set ingress.hostname=your-rancher-domain.com
```

### Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `baseURL` | Base URL for OIDC endpoints (required) | `""` |
| `port` | HTTP server port | `8080` |
| `cacheTTL` | Cache TTL in minutes | `15` |
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.hostname` | Ingress hostname | `""` |
| `ingress.className` | Ingress class name | `""` |
| `image.repository` | Container image repository | `ghcr.io/ekristen/rancher-oidc` |
| `image.tag` | Container image tag | `""` (uses appVersion) |
| `serviceAccount.create` | Create service account | `true` |
| `rbac.create` | Create RBAC resources | `true` |

## Usage

### Cluster IDs

The aggregator uses Rancher's internal cluster IDs (e.g., `c-m-xxxxx`) which are found in the `status.clusterName` field of `provisioning.cattle.io/v1 Cluster` resources.

**Special cluster ID:**
- `local` - Always refers to the management cluster where the aggregator is running

### API Endpoints

#### OIDC Discovery Document

```
GET /oidc/{cluster_id}/.well-known/openid-configuration
```

Example:
```bash
curl https://rancher.example.com/oidc/c-m-abc123/.well-known/openid-configuration
```

Response:
```json
{
  "issuer": "https://rancher.example.com/oidc/c-m-abc123",
  "jwks_uri": "https://rancher.example.com/oidc/c-m-abc123/jwks",
  "response_types_supported": ["id_token"],
  "subject_types_supported": ["public"],
  "id_token_signing_alg_values_supported": ["RS256"]
}
```

#### JWKS Document

```
GET /oidc/{cluster_id}/jwks
```

Example:
```bash
curl https://rancher.example.com/oidc/c-m-abc123/jwks
```

Response:
```json
{
  "keys": [
    {
      "kty": "RSA",
      "kid": "abc123",
      "use": "sig",
      "alg": "RS256",
      "n": "...",
      "e": "AQAB"
    }
  ]
}
```

#### Health Check

```
GET /healthz
```

Response:
```json
{
  "success": true,
  "data": {
    "status": "healthy",
    "cached_clusters": 5,
    "available_clusters": 10,
    "local_cluster_ready": true,
    "cache_ttl": "15m0s",
    "timestamp": "2024-01-15T10:30:00Z",
    "storage_status": "ok"
  }
}
```

## RBAC

The service account requires the following permissions:

```yaml
# Access Rancher cluster resources
- apiGroups: ["provisioning.cattle.io"]
  resources: ["clusters"]
  verbs: ["get", "list", "watch"]

# Access secrets (kubeconfigs and cache)
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

# Access namespaces
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list"]
```

## Development

### Running Locally

```bash
# Build
go build -o rancher-rancher2-oidc .

# Run with KUBECONFIG (connects to Rancher cluster)
export KUBECONFIG=~/.kube/rancher-config
./rancher-rancher2-oidc aggregator --base-url http://localhost:8080

# Or specify kubeconfig via flag
./rancher-rancher2-oidc aggregator \
  --base-url http://localhost:8080 \
  --kubeconfig ~/.kube/rancher-config
```

Note: When running outside a cluster, the `local` cluster endpoint will not be available since it requires in-cluster configuration.

### CLI Flags

```
--base-url        Base URL for OIDC endpoints (required)
--port, -p        HTTP server port (default: 8080)
--cache-ttl       Cache TTL in minutes (default: 15)
--kubeconfig      Path to kubeconfig file (uses in-cluster if not specified)
--cert-file       TLS certificate file path
--key-file        TLS private key file path
--log-level, -l   Log level (default: info)
--log-format      Log format: auto, json, console (default: auto)
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Commit your changes (`git commit -am 'Add new feature'`)
4. Push to the branch (`git push origin feature/my-feature`)
5. Create a Pull Request

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
