package audittrail

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// SecretProvider defines interface for loading secrets from various providers
type SecretProvider interface {
	GetSecret(ctx context.Context, key string) (string, error)
}

// GCPSecretProvider loads secrets from Google Cloud Secret Manager
type GCPSecretProvider struct {
	client    *secretmanager.Client
	projectID string
}

// NewGCPSecretProvider creates a new GCP Secret Manager provider
func NewGCPSecretProvider(ctx context.Context, projectID string) (*GCPSecretProvider, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create secret manager client: %w", err)
	}

	return &GCPSecretProvider{
		client:    client,
		projectID: projectID,
	}, nil
}

// GetSecret retrieves a secret from GCP Secret Manager
func (p *GCPSecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
	if p == nil || p.client == nil {
		return "", fmt.Errorf("GCP secret provider not initialized")
	}

	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", p.projectID, key)

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	}

	result, err := p.client.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to access secret %s: %w", key, err)
	}

	return string(result.Payload.Data), nil
}

// Close closes the GCP Secret Manager client
func (p *GCPSecretProvider) Close() error {
	if p != nil && p.client != nil {
		return p.client.Close()
	}
	return nil
}

// AWSSecretProvider loads secrets from AWS Secrets Manager
type AWSSecretProvider struct {
	// Client will be added when implementing AWS support
	region string
}

// NewAWSSecretProvider creates a new AWS Secrets Manager provider
// Note: Requires AWS SDK to be implemented
func NewAWSSecretProvider(region string) (*AWSSecretProvider, error) {
	return &AWSSecretProvider{
		region: region,
	}, nil
}

// GetSecret retrieves a secret from AWS Secrets Manager
func (p *AWSSecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
	// TODO: Implement AWS Secrets Manager integration
	// Requires: github.com/aws/aws-sdk-go-v2/service/secretsmanager
	return "", fmt.Errorf("AWS Secrets Manager not yet implemented")
}

// MapSecretProvider maps environment variable keys to secret names
type MapSecretProvider struct {
	secrets map[string]string
}

// NewMapSecretProvider creates a provider from a static map (useful for testing)
func NewMapSecretProvider(secrets map[string]string) *MapSecretProvider {
	return &MapSecretProvider{
		secrets: secrets,
	}
}

// GetSecret retrieves a secret from the map
func (p *MapSecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
	if val, ok := p.secrets[key]; ok {
		return val, nil
	}
	return "", fmt.Errorf("secret %s not found", key)
}
