package provideradapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	ErrUnsupportedProvider  = errors.New("unsupported provider adapter")
	ErrInvalidConfiguration = errors.New("invalid provider adapter configuration")
)

type CredentialResolver interface {
	AWSCredentials(context.Context, controlplane.GatewayProvider) (aws.Credentials, error)
	GCPToken(context.Context, controlplane.GatewayProvider) (string, error)
	AzureToken(context.Context, controlplane.GatewayProvider) (string, error)
}

type Registry struct {
	credentials CredentialResolver
	now         func() time.Time
}

func NewRegistry(credentials CredentialResolver) *Registry {
	if credentials == nil {
		credentials = DefaultCredentialResolver{}
	}
	return &Registry{credentials: credentials, now: time.Now}
}

func (r *Registry) BuildRequest(ctx context.Context, provider controlplane.GatewayProvider, body []byte, stream bool) (*http.Request, error) {
	if r == nil {
		r = NewRegistry(nil)
	}
	switch provider.Type {
	case controlplane.ProviderTypeOpenAICompatible, controlplane.ProviderTypeAnthropicCompatible, controlplane.ProviderTypeGeminiCompatible:
		return r.compatibleRequest(ctx, provider, body, stream)
	case controlplane.ProviderTypeAWSBedrock:
		return r.bedrockRequest(ctx, provider, body, stream)
	case controlplane.ProviderTypeGCPVertex:
		return r.vertexRequest(ctx, provider, body, stream)
	case controlplane.ProviderTypeAzureOpenAI:
		return r.azureRequest(ctx, provider, body, stream)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider.Type)
	}
}

func (r *Registry) compatibleRequest(ctx context.Context, provider controlplane.GatewayProvider, body []byte, stream bool) (*http.Request, error) {
	endpoint, err := compatibleEndpoint(provider, stream)
	if err != nil {
		return nil, err
	}
	req, err := newJSONRequest(ctx, endpoint, body, stream)
	if err != nil {
		return nil, err
	}
	switch provider.Type {
	case controlplane.ProviderTypeAnthropicCompatible:
		req.Header.Set("x-api-key", provider.APIKey)
		version := provider.AdapterConfig["anthropic_version"]
		if version == "" {
			version = "2023-06-01"
		}
		req.Header.Set("anthropic-version", version)
	case controlplane.ProviderTypeGeminiCompatible:
		req.Header.Set("x-goog-api-key", provider.APIKey)
	default:
		if provider.AuthType == controlplane.ProviderAuthAPIKey {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		}
	}
	return req, nil
}

func compatibleEndpoint(provider controlplane.GatewayProvider, stream bool) (string, error) {
	base := strings.TrimRight(provider.BaseURL, "/")
	switch provider.UpstreamFormat {
	case controlplane.UpstreamFormatOpenAIChat:
		return base + "/chat/completions", nil
	case controlplane.UpstreamFormatOpenAIResponses:
		return base + "/responses", nil
	case controlplane.UpstreamFormatAnthropic:
		return base + "/messages", nil
	case controlplane.UpstreamFormatGemini:
		operation := "generateContent"
		if stream {
			operation = "streamGenerateContent"
		}
		endpoint := base + "/models/" + url.PathEscape(provider.UpstreamModel) + ":" + operation
		if stream {
			endpoint += "?alt=sse"
		}
		return endpoint, nil
	default:
		return "", fmt.Errorf("%w: unsupported upstream_format %q", ErrInvalidConfiguration, provider.UpstreamFormat)
	}
}

func (r *Registry) bedrockRequest(ctx context.Context, provider controlplane.GatewayProvider, body []byte, stream bool) (*http.Request, error) {
	region := strings.TrimSpace(provider.AdapterConfig["region"])
	if region == "" {
		return nil, fmt.Errorf("%w: adapter_config.region is required", ErrInvalidConfiguration)
	}
	base := strings.TrimRight(strings.TrimSpace(provider.AdapterConfig["endpoint"]), "/")
	if base == "" {
		base = strings.TrimRight(provider.BaseURL, "/")
	}
	operation := "converse"
	if stream {
		operation = "converse-stream"
	}
	endpoint := base + "/model/" + url.PathEscape(provider.UpstreamModel) + "/" + operation
	req, err := newJSONRequest(ctx, endpoint, body, stream)
	if err != nil {
		return nil, err
	}
	if stream {
		req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	}
	resolved, err := r.credentials.AWSCredentials(ctx, provider)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(digest[:])
	if err := awsv4.NewSigner().SignHTTP(ctx, resolved, req, payloadHash, "bedrock", region, r.now().UTC()); err != nil {
		return nil, fmt.Errorf("sign Bedrock request: %w", err)
	}
	return req, nil
}

func (r *Registry) vertexRequest(ctx context.Context, provider controlplane.GatewayProvider, body []byte, stream bool) (*http.Request, error) {
	project := strings.TrimSpace(provider.AdapterConfig["project"])
	location := strings.TrimSpace(provider.AdapterConfig["location"])
	if project == "" || location == "" {
		return nil, fmt.Errorf("%w: adapter_config.project and location are required", ErrInvalidConfiguration)
	}
	base := strings.TrimRight(strings.TrimSpace(provider.AdapterConfig["endpoint"]), "/")
	if base == "" {
		base = strings.TrimRight(provider.BaseURL, "/")
	}
	var endpoint string
	switch provider.UpstreamFormat {
	case controlplane.UpstreamFormatAnthropic:
		operation := "rawPredict"
		if stream {
			operation = "streamRawPredict"
		}
		endpoint = fmt.Sprintf("%s/%s/%s/locations/%s/publishers/anthropic/models/%s:%s", base, "projects", url.PathEscape(project), url.PathEscape(location), url.PathEscape(provider.UpstreamModel), operation)
		body = vertexAnthropicBody(body)
	case controlplane.UpstreamFormatGemini:
		operation := "generateContent"
		if stream {
			operation = "streamGenerateContent"
		}
		endpoint = fmt.Sprintf("%s/%s/%s/locations/%s/publishers/google/models/%s:%s", base, "projects", url.PathEscape(project), url.PathEscape(location), url.PathEscape(provider.UpstreamModel), operation)
		if stream {
			endpoint += "?alt=sse"
		}
	default:
		return nil, fmt.Errorf("%w: Vertex does not support upstream_format %q", ErrInvalidConfiguration, provider.UpstreamFormat)
	}
	token, err := r.credentials.GCPToken(ctx, provider)
	if err != nil {
		return nil, err
	}
	req, err := newJSONRequest(ctx, endpoint, body, stream)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

func vertexAnthropicBody(body []byte) []byte {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	delete(payload, "model")
	payload["anthropic_version"] = "vertex-2023-10-16"
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func (r *Registry) azureRequest(ctx context.Context, provider controlplane.GatewayProvider, body []byte, stream bool) (*http.Request, error) {
	apiVersion := strings.TrimSpace(provider.AdapterConfig["api_version"])
	if apiVersion == "" {
		return nil, fmt.Errorf("%w: adapter_config.api_version is required", ErrInvalidConfiguration)
	}
	baseURL, err := url.Parse(strings.TrimRight(provider.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("%w: Azure endpoint", ErrInvalidConfiguration)
	}
	switch provider.UpstreamFormat {
	case controlplane.UpstreamFormatOpenAIChat:
		baseURL.Path = path.Join(baseURL.Path, "openai", "deployments", provider.UpstreamModel, "chat", "completions")
	case controlplane.UpstreamFormatOpenAIResponses:
		baseURL.Path = path.Join(baseURL.Path, "openai", "v1", "responses")
	default:
		return nil, fmt.Errorf("%w: Azure does not support upstream_format %q", ErrInvalidConfiguration, provider.UpstreamFormat)
	}
	query := baseURL.Query()
	query.Set("api-version", apiVersion)
	baseURL.RawQuery = query.Encode()
	req, err := newJSONRequest(ctx, baseURL.String(), body, stream)
	if err != nil {
		return nil, err
	}
	switch provider.AuthType {
	case controlplane.ProviderAuthAPIKey:
		req.Header.Set("api-key", provider.APIKey)
	case controlplane.ProviderAuthAzureManagedIdentity:
		token, tokenErr := r.credentials.AzureToken(ctx, provider)
		if tokenErr != nil {
			return nil, tokenErr
		}
		req.Header.Set("Authorization", "Bearer "+token)
	default:
		return nil, fmt.Errorf("%w: Azure auth_type %q", ErrInvalidConfiguration, provider.AuthType)
	}
	return req, nil
}

func newJSONRequest(ctx context.Context, endpoint string, body []byte, stream bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return req, nil
}

type azureTokenCredential interface {
	GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error)
}

type DefaultCredentialResolver struct {
	newDefaultAzureCredential func() (azureTokenCredential, error)
	newManagedIdentity        func(string) (azureTokenCredential, error)
}

func (DefaultCredentialResolver) AWSCredentials(ctx context.Context, provider controlplane.GatewayProvider) (aws.Credentials, error) {
	region := provider.AdapterConfig["region"]
	switch provider.AuthType {
	case controlplane.ProviderAuthAWSDefault:
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return aws.Credentials{}, fmt.Errorf("load AWS default credentials: %w", err)
		}
		return cfg.Credentials.Retrieve(ctx)
	case controlplane.ProviderAuthAWSAccessKey:
		var secret struct {
			AccessKeyID     string `json:"access_key_id"`
			SecretAccessKey string `json:"secret_access_key"`
			SessionToken    string `json:"session_token"`
		}
		if json.Unmarshal([]byte(provider.APIKey), &secret) != nil || secret.AccessKeyID == "" || secret.SecretAccessKey == "" {
			return aws.Credentials{}, fmt.Errorf("%w: AWS access key credential must be JSON", ErrInvalidConfiguration)
		}
		return credentials.NewStaticCredentialsProvider(secret.AccessKeyID, secret.SecretAccessKey, secret.SessionToken).Retrieve(ctx)
	default:
		return aws.Credentials{}, fmt.Errorf("%w: AWS auth_type %q", ErrInvalidConfiguration, provider.AuthType)
	}
}

func (DefaultCredentialResolver) GCPToken(ctx context.Context, provider controlplane.GatewayProvider) (string, error) {
	const scope = "https://www.googleapis.com/auth/cloud-platform"
	var source oauth2.TokenSource
	switch provider.AuthType {
	case controlplane.ProviderAuthGCPADC:
		credentials, err := google.FindDefaultCredentials(ctx, scope)
		if err != nil {
			return "", fmt.Errorf("load GCP ADC: %w", err)
		}
		source = credentials.TokenSource
	case controlplane.ProviderAuthGCPServiceAccount:
		config, err := google.JWTConfigFromJSON([]byte(provider.APIKey), scope)
		if err != nil {
			return "", fmt.Errorf("load GCP service account: %w", err)
		}
		source = config.TokenSource(ctx)
	default:
		return "", fmt.Errorf("%w: GCP auth_type %q", ErrInvalidConfiguration, provider.AuthType)
	}
	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("resolve GCP access token: %w", err)
	}
	return token.AccessToken, nil
}

func (r DefaultCredentialResolver) AzureToken(ctx context.Context, provider controlplane.GatewayProvider) (string, error) {
	if provider.AuthType != controlplane.ProviderAuthAzureManagedIdentity {
		return "", fmt.Errorf("%w: Azure auth_type %q", ErrInvalidConfiguration, provider.AuthType)
	}
	audience := strings.TrimSpace(provider.AdapterConfig["audience"])
	if audience == "" {
		audience = "https://cognitiveservices.azure.com/.default"
	}
	clientID := strings.TrimSpace(provider.AdapterConfig["managed_identity_client_id"])

	var (
		credential azureTokenCredential
		err        error
	)
	if clientID != "" {
		factory := r.newManagedIdentity
		if factory == nil {
			factory = newAzureManagedIdentityCredential
		}
		credential, err = factory(clientID)
	} else {
		factory := r.newDefaultAzureCredential
		if factory == nil {
			factory = newAzureDefaultCredential
		}
		credential, err = factory()
	}
	if err != nil {
		return "", fmt.Errorf("create Azure credential: %w", err)
	}
	token, err := credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{audience}})
	if err != nil {
		return "", fmt.Errorf("resolve Azure access token: %w", err)
	}
	if strings.TrimSpace(token.Token) == "" {
		return "", errors.New("resolve Azure access token: credential returned an empty token")
	}
	return token.Token, nil
}

func newAzureDefaultCredential() (azureTokenCredential, error) {
	return azidentity.NewDefaultAzureCredential(nil)
}

func newAzureManagedIdentityCredential(clientID string) (azureTokenCredential, error) {
	return azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(clientID)})
}
