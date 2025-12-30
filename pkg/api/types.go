package api

import (
	"encoding/json"
	"time"
)

// OIDCConfiguration represents the OIDC discovery document
type OIDCConfiguration struct {
	Issuer                           string   `json:"issuer"`
	JWKSURI                          string   `json:"jwks_uri"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
}

// JWKSKey represents a single key in the JWKS document
type JWKSKey struct {
	KTY string `json:"kty"`
	KID string `json:"kid"`
	Use string `json:"use"`
	ALG string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKS represents the JSON Web Key Set document
type JWKS struct {
	Keys []JWKSKey `json:"keys"`
}

// ClusterIdentity represents the combined OIDC and JWKS data for a cluster
type ClusterIdentity struct {
	ClusterID  string            `json:"cluster_id"`
	OIDCConfig OIDCConfiguration `json:"oidc_config"`
	JWKS       JWKS              `json:"jwks"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// APIResponse represents a generic API response structure
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// IdentityUpdateRequest represents the request body for updating cluster identity
type IdentityUpdateRequest struct {
	OIDCConfig json.RawMessage `json:"openid_configuration"`
	JWKS       json.RawMessage `json:"jwks"`
}
