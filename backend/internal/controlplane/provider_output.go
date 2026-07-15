package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrProviderOutputInvalid           = errors.New("provider output descriptor is invalid")
	ErrProviderOutputsRequired         = errors.New("provider succeeded without a deliverable final output")
	ErrProviderOutputReaderRequired    = errors.New("provider output reader is required")
	ErrProviderOutputReferenceRequired = errors.New("provider output reference is required")
	ErrProviderOutputPolicyUnsupported = errors.New("artifact policy cannot deliver durable provider output")
)

// ProviderOutputDescriptor contains provider facts only. It cannot select the
// Core store, retention, authorization, or delivery target.
type ProviderOutputDescriptor struct {
	OutputID            string `json:"output_id"`
	Role                string `json:"role"`
	MediaType           string `json:"media_type"`
	ExpectedSizeBytes   int64  `json:"expected_size_bytes"`
	ExpectedSHA256      string `json:"expected_sha256,omitempty"`
	ProviderReference   string `json:"provider_reference,omitempty"`
	PersistentReference bool   `json:"persistent_reference,omitempty"`
}

type DurableAIJobOutputReader interface {
	OpenProviderOutput(context.Context, GatewayProvider, AIJob, AIAttempt, ProviderOutputDescriptor) (io.ReadCloser, error)
}

func normalizeProviderOutputs(outputs []ProviderOutputDescriptor) ([]ProviderOutputDescriptor, error) {
	normalized := make([]ProviderOutputDescriptor, 0, len(outputs))
	seen := make(map[string]struct{}, len(outputs))
	for _, output := range outputs {
		output.OutputID = strings.TrimSpace(output.OutputID)
		output.Role = strings.TrimSpace(output.Role)
		output.MediaType = strings.TrimSpace(output.MediaType)
		output.ExpectedSHA256 = strings.ToLower(strings.TrimSpace(output.ExpectedSHA256))
		output.ProviderReference = strings.TrimSpace(output.ProviderReference)
		if !validProviderOutputID(output.OutputID) || !oneOf(output.Role, ArtifactRolePreview, ArtifactRoleFinal, ArtifactRoleDerived, ArtifactRoleMetadata, ArtifactRoleProviderReference) || output.ExpectedSizeBytes < -1 {
			return nil, ErrProviderOutputInvalid
		}
		if output.ExpectedSHA256 != "" {
			decoded, err := hex.DecodeString(output.ExpectedSHA256)
			if err != nil || len(decoded) != sha256.Size {
				return nil, ErrProviderOutputInvalid
			}
		}
		if output.PersistentReference && output.ProviderReference == "" {
			return nil, ErrProviderOutputInvalid
		}
		if _, duplicate := seen[output.OutputID]; duplicate {
			return nil, ErrProviderOutputInvalid
		}
		seen[output.OutputID] = struct{}{}
		normalized = append(normalized, output)
	}
	return normalized, nil
}

func validProviderOutputID(id string) bool {
	if id == "" || len(id) > 160 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-_.:", r) {
			continue
		}
		return false
	}
	return true
}

func providerOutputArtifactID(attemptID, outputID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(attemptID) + "\n" + strings.TrimSpace(outputID)))
	return "artifact_output_" + hex.EncodeToString(digest[:16])
}

func (s *Service) ingestProviderOutputs(ctx context.Context, provider GatewayProvider, job AIJob, attempt AIAttempt, outputs []ProviderOutputDescriptor, adapter DurableAIJobAdapter) ([]Artifact, error) {
	normalized, err := normalizeProviderOutputs(outputs)
	if err != nil {
		return nil, err
	}
	artifacts := make([]Artifact, 0, len(normalized))
	for _, output := range normalized {
		artifact, err := s.ensureProviderOutputArtifact(ctx, provider, job, attempt, output, adapter)
		if err != nil {
			return artifacts, providerOutputError(output, err)
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func (s *Service) ensureProviderOutputArtifact(ctx context.Context, provider GatewayProvider, job AIJob, attempt AIAttempt, output ProviderOutputDescriptor, adapter DurableAIJobAdapter) (Artifact, error) {
	artifactID := providerOutputArtifactID(attempt.ID, output.OutputID)
	input := ArtifactCreateInput{
		ID: artifactID, OperationID: job.OperationID, JobID: job.ID, AttemptID: attempt.ID,
		Role: output.Role, Policy: job.ArtifactPolicy, MediaType: output.MediaType,
		ExternalReference: output.ProviderReference, ExpectedSizeBytes: output.ExpectedSizeBytes,
		ExpectedSHA256: output.ExpectedSHA256, MaxBytes: ArtifactDefaultMaxBytes,
	}
	existing, found, err := s.repo.FindArtifact(ctx, artifactID)
	if err != nil {
		return Artifact{}, err
	}
	if found {
		if err := validateProviderOutputArtifact(existing, input); err != nil {
			return Artifact{}, err
		}
		if existing.Status == ArtifactStatusDelivered || (!oneOf(job.ArtifactPolicy, GatewayArtifactPolicyCustomerSink, GatewayArtifactPolicyProxyOnly) && existing.Status == ArtifactStatusReady) {
			return existing, nil
		}
	}

	switch job.ArtifactPolicy {
	case GatewayArtifactPolicyMetadataOnly:
		if !output.PersistentReference {
			return Artifact{}, ErrProviderOutputPolicyUnsupported
		}
		if found {
			if existing.Status == ArtifactStatusPending {
				sizeBytes := input.ExpectedSizeBytes
				if sizeBytes < 0 {
					sizeBytes = 0
				}
				return s.transitionArtifact(ctx, existing, ArtifactStatusReady, "metadata_recovered", &ArtifactContentUpdate{
					MediaType: input.MediaType, SizeBytes: sizeBytes, SHA256: input.ExpectedSHA256,
					StoreDriver: ArtifactStoreDriverNone, ExternalReference: input.ExternalReference,
				})
			}
			return Artifact{}, ErrArtifactIngestInProgress
		}
		return s.createProviderOutputArtifact(ctx, input, nil)
	case GatewayArtifactPolicyTemporary, GatewayArtifactPolicyManaged:
		driver, configured := s.primaryArtifactStoreDriver()
		if !configured {
			return Artifact{}, ErrArtifactStoreRequired
		}
		input.StoreDriver = driver
		readerAdapter, ok := adapter.(DurableAIJobOutputReader)
		if !ok {
			return Artifact{}, ErrProviderOutputReaderRequired
		}
		reader := &providerOutputReader{ctx: ctx, adapter: readerAdapter, provider: provider, job: job, attempt: attempt, output: output}
		defer reader.Close()
		if found {
			return s.resumeArtifactContent(ctx, existing, input, reader)
		}
		return s.createProviderOutputArtifact(ctx, input, reader)
	case GatewayArtifactPolicyCustomerSink:
		return s.ensureCustomerSinkProviderOutput(ctx, provider, job, attempt, output, input, existing, found, adapter)
	case GatewayArtifactPolicyProxyOnly:
		return s.ensureProxyProviderOutput(ctx, provider, input, existing, found, output)
	default:
		return Artifact{}, ErrProviderOutputPolicyUnsupported
	}
}

func (s *Service) createProviderOutputArtifact(ctx context.Context, input ArtifactCreateInput, body io.Reader) (Artifact, error) {
	artifact, err := s.CreateArtifactFromReader(ctx, input, body)
	if err == nil {
		return artifact, nil
	}
	existing, found, findErr := s.repo.FindArtifact(ctx, input.ID)
	if findErr != nil {
		return Artifact{}, errors.Join(err, findErr)
	}
	if !found {
		return artifact, err
	}
	if validationErr := validateProviderOutputArtifact(existing, input); validationErr != nil {
		return Artifact{}, errors.Join(err, validationErr)
	}
	if oneOf(existing.Status, ArtifactStatusReady, ArtifactStatusDelivered) {
		return existing, nil
	}
	if existing.Status == ArtifactStatusUploading {
		return existing, ErrArtifactIngestInProgress
	}
	return existing, err
}

func validateProviderOutputArtifact(artifact Artifact, input ArtifactCreateInput) error {
	if artifact.ID != input.ID || artifact.OperationID != input.OperationID || artifact.JobID != input.JobID || artifact.AttemptID != input.AttemptID ||
		artifact.Role != input.Role || artifact.Policy != input.Policy || artifact.MediaType != strings.TrimSpace(input.MediaType) {
		return ErrArtifactIntegrity
	}
	if input.Policy == GatewayArtifactPolicyMetadataOnly && artifact.ExternalReference != strings.TrimSpace(input.ExternalReference) {
		return ErrArtifactIntegrity
	}
	if input.ExpectedSizeBytes > 0 && oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivered) && artifact.SizeBytes != input.ExpectedSizeBytes {
		return ErrArtifactIntegrity
	}
	if expected := strings.ToLower(strings.TrimSpace(input.ExpectedSHA256)); expected != "" && oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivered) && artifact.SHA256 != expected {
		return ErrArtifactIntegrity
	}
	return nil
}

func providerOutputsDeliverable(job AIJob, attemptID string, artifacts []Artifact) error {
	if !oneOf(strings.ToLower(strings.TrimSpace(job.Modality)), "image", "video", "audio") {
		return nil
	}
	for _, artifact := range artifacts {
		deliverable := oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivered)
		if job.ArtifactPolicy == GatewayArtifactPolicyCustomerSink {
			deliverable = artifact.Status == ArtifactStatusDelivered
		}
		if artifact.JobID == job.ID && artifact.AttemptID == strings.TrimSpace(attemptID) && artifact.Role == ArtifactRoleFinal && deliverable {
			return nil
		}
	}
	return ErrProviderOutputsRequired
}

type providerOutputReader struct {
	ctx      context.Context
	adapter  DurableAIJobOutputReader
	provider GatewayProvider
	job      AIJob
	attempt  AIAttempt
	output   ProviderOutputDescriptor
	body     io.ReadCloser
	openErr  error
}

func (r *providerOutputReader) Read(p []byte) (int, error) {
	if r.body == nil && r.openErr == nil {
		r.body, r.openErr = r.adapter.OpenProviderOutput(r.ctx, r.provider, r.job, r.attempt, r.output)
		if r.openErr == nil && r.body == nil {
			r.openErr = ErrProviderOutputReaderRequired
		}
	}
	if r.openErr != nil {
		return 0, r.openErr
	}
	return r.body.Read(p)
}

func (r *providerOutputReader) Close() error {
	if r.body == nil {
		return nil
	}
	return r.body.Close()
}

func providerOutputError(output ProviderOutputDescriptor, err error) error {
	return fmt.Errorf("ingest provider output %q: %w", output.OutputID, err)
}
