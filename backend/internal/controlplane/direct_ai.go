package controlplane

import (
	"context"
	"errors"
	"io"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

var ErrDirectAIAdapterRequired = errors.New("direct ai provider adapter is required")

// DirectAIProviderAdapter executes a provider call in the current request.
// Core still owns routing, capacity, attempts, artifacts, usage, and billing.
type DirectAIProviderAdapter interface {
	SelectDirectAIAdapter(context.Context, GatewayProvider, gatewaycore.CanonicalRequest, string) (adapterID string, supported bool, err error)
	DispatchDirectAI(context.Context, GatewayProvider, AIOperation, AIAttempt, gatewaycore.CanonicalRequest, ProviderDispatchCommand) (ProviderDispatchResult, error)
	OpenDirectAIOutput(context.Context, GatewayProvider, AIOperation, AIAttempt, gatewaycore.CanonicalRequest, ProviderDispatchResult, ProviderOutputDescriptor) (io.ReadCloser, error)
}

func (s *Service) IngestDirectAIProviderOutputs(
	ctx context.Context,
	provider GatewayProvider,
	operation AIOperation,
	attempt AIAttempt,
	request gatewaycore.CanonicalRequest,
	result ProviderDispatchResult,
	adapter DirectAIProviderAdapter,
) ([]Artifact, error) {
	if adapter == nil {
		return nil, ErrDirectAIAdapterRequired
	}
	job := AIJob{
		OperationID: operation.ID, ProfileScope: operation.ProfileScope, TenantID: operation.TenantID,
		CredentialID: operation.CredentialID, CredentialSource: operation.CredentialSource,
		IntegrationID: operation.IntegrationID, PrincipalType: operation.PrincipalType, PrincipalID: operation.PrincipalID,
		ExternalSubjectReference: operation.ExternalSubjectReference, Protocol: string(request.Protocol),
		Operation: request.Operation, Modality: request.Modality, Model: request.Model,
		ArtifactPolicy: operation.ArtifactPolicy, ArtifactSinkID: operation.ArtifactSinkID,
	}
	bridge := &directAIOutputBridge{adapter: adapter, operation: operation, request: request, result: result}
	return s.ingestProviderOutputs(ctx, provider, job, attempt, result.Outputs, bridge)
}

func (s *Service) DirectArtifactsForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, operationID string) ([]Artifact, error) {
	owner := ArtifactOwner(aiJobOwnerFromAuth(auth))
	return s.repo.QueryArtifacts(ctx, ArtifactQuery{Owner: &owner, OperationID: operationID, Limit: 100})
}

type directAIOutputBridge struct {
	adapter   DirectAIProviderAdapter
	operation AIOperation
	request   gatewaycore.CanonicalRequest
	result    ProviderDispatchResult
}

func (*directAIOutputBridge) DispatchProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchCommand) (ProviderDispatchResult, error) {
	return ProviderDispatchResult{}, ErrDirectAIAdapterRequired
}

func (*directAIOutputBridge) ReconcileProviderTask(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderDispatchIntent, ProviderTaskReference) (ProviderDispatchResult, error) {
	return ProviderDispatchResult{}, ErrDirectAIAdapterRequired
}

func (bridge *directAIOutputBridge) OpenProviderOutput(ctx context.Context, provider GatewayProvider, _ AIJob, attempt AIAttempt, output ProviderOutputDescriptor) (io.ReadCloser, error) {
	return bridge.adapter.OpenDirectAIOutput(ctx, provider, bridge.operation, attempt, bridge.request, bridge.result, output)
}

var (
	_ DurableAIJobAdapter      = (*directAIOutputBridge)(nil)
	_ DurableAIJobOutputReader = (*directAIOutputBridge)(nil)
)
