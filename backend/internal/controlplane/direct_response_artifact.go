package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// StoreDirectResponseArtifact applies the operation's immutable artifact
// policy to a response already received by the Direct HTTP lane.
func (s *Service) StoreDirectResponseArtifact(ctx context.Context, operation AIOperation, attempt AIAttempt, outputID, mediaType string, content []byte) (Artifact, error) {
	if operation.ID == "" || attempt.ID == "" || attempt.OperationID != operation.ID || strings.TrimSpace(outputID) == "" || strings.TrimSpace(mediaType) == "" || len(content) == 0 {
		return Artifact{}, ErrProviderOutputInvalid
	}
	if operation.ArtifactPolicy == GatewayArtifactPolicyProxyOnly {
		return Artifact{}, nil
	}
	digest := sha256.Sum256(content)
	input := ArtifactCreateInput{
		ID: providerOutputArtifactID(attempt.ID, outputID), OperationID: operation.ID, AttemptID: attempt.ID,
		Role: ArtifactRoleFinal, Policy: operation.ArtifactPolicy, MediaType: strings.TrimSpace(mediaType),
		ExpectedSizeBytes: int64(len(content)), ExpectedSHA256: hex.EncodeToString(digest[:]), MaxBytes: ArtifactDefaultMaxBytes,
	}
	switch operation.ArtifactPolicy {
	case GatewayArtifactPolicyMetadataOnly:
		return s.createProviderOutputArtifact(ctx, input, nil)
	case GatewayArtifactPolicyTemporary, GatewayArtifactPolicyManaged:
		driver, configured := s.primaryArtifactStoreDriver()
		if !configured {
			return Artifact{}, ErrArtifactStoreRequired
		}
		input.StoreDriver = driver
		return s.createProviderOutputArtifact(ctx, input, bytes.NewReader(content))
	case GatewayArtifactPolicyCustomerSink:
		sink, configured := s.artifactSink(operation.ArtifactSinkID)
		if !configured {
			return Artifact{}, ErrArtifactSinkRequired
		}
		owner := artifactOwnerFromOperation(operation)
		if !sink.Accepts(owner) {
			return Artifact{}, ErrArtifactSinkForbidden
		}
		artifact, err := s.createProviderOutputArtifact(ctx, input, nil)
		if err != nil {
			return artifact, err
		}
		return s.deliverArtifactToCustomerSink(ctx, artifact, input, bytes.NewReader(content), sink, owner)
	default:
		return Artifact{}, ErrProviderOutputPolicyUnsupported
	}
}
