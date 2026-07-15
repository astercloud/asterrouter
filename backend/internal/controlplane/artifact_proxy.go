package controlplane

import (
	"context"
	"errors"
	"io"
	"strings"
)

var (
	ErrArtifactProxyRequired = errors.New("artifact provider proxy is required")
	ErrArtifactProxyInvalid  = errors.New("artifact provider proxy is invalid")
)

type ArtifactProxyRequest struct {
	ArtifactID        string
	OperationID       string
	JobID             string
	AttemptID         string
	ProviderID        string
	ProviderAccountID string
	ProviderTaskID    string
	ProviderRequestID string
	ProviderReference string
	Role              string
	MediaType         string
	ExpectedSizeBytes int64
	ExpectedSHA256    string
	Owner             ArtifactOwner
}

// ArtifactProxy is a plugin-owned reader for provider-retained output. It must
// honor byte ranges and validate provider bytes against the expected metadata.
type ArtifactProxy interface {
	ProviderID() string
	OpenArtifact(context.Context, ArtifactProxyRequest, *ArtifactByteRange) (ArtifactRead, error)
}

func (s *Service) SetArtifactProxy(proxy ArtifactProxy) error {
	if proxy == nil || strings.TrimSpace(proxy.ProviderID()) == "" {
		return ErrArtifactProxyInvalid
	}
	s.artifactProxyMu.Lock()
	defer s.artifactProxyMu.Unlock()
	s.artifactProxies[strings.TrimSpace(proxy.ProviderID())] = proxy
	return nil
}

func (s *Service) artifactProxy(providerID string) (ArtifactProxy, bool) {
	s.artifactProxyMu.RLock()
	defer s.artifactProxyMu.RUnlock()
	proxy, found := s.artifactProxies[strings.TrimSpace(providerID)]
	return proxy, found
}

func (s *Service) ensureProxyProviderOutput(ctx context.Context, provider GatewayProvider, input ArtifactCreateInput, existing Artifact, found bool, output ProviderOutputDescriptor) (Artifact, error) {
	if strings.TrimSpace(output.ProviderReference) == "" {
		return Artifact{}, ErrProviderOutputReferenceRequired
	}
	if _, configured := s.artifactProxy(provider.ID); !configured {
		return Artifact{}, ErrArtifactProxyRequired
	}
	if found {
		if existing.Status == ArtifactStatusReady {
			return existing, nil
		}
		if existing.Status == ArtifactStatusPending {
			sizeBytes := input.ExpectedSizeBytes
			if sizeBytes < 0 {
				sizeBytes = 0
			}
			return s.transitionArtifact(ctx, existing, ArtifactStatusReady, "proxy_reference_recovered", &ArtifactContentUpdate{
				MediaType: input.MediaType, SizeBytes: sizeBytes, SHA256: input.ExpectedSHA256,
				StoreDriver: ArtifactStoreDriverNone, ExternalReference: input.ExternalReference,
			})
		}
		return Artifact{}, ErrArtifactIngestInProgress
	}
	return s.createProviderOutputArtifact(ctx, input, nil)
}

func (s *Service) openProxiedArtifact(ctx context.Context, artifact Artifact, byteRange *ArtifactByteRange) (ArtifactRead, error) {
	if artifact.Status != ArtifactStatusReady || !artifact.RetainUntil.After(s.nowUTC()) || strings.TrimSpace(artifact.ExternalReference) == "" {
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	job, found, err := s.repo.FindAIJob(ctx, artifact.JobID)
	if err != nil || !found || !aiJobOwnerMatches(job, artifactOwnerFromOperationLikeArtifact(artifact)) {
		if err != nil {
			return ArtifactRead{}, err
		}
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	attempt, found, err := s.repo.FindAIAttempt(ctx, artifact.AttemptID)
	if err != nil || !found || attempt.OperationID != artifact.OperationID {
		if err != nil {
			return ArtifactRead{}, err
		}
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	proxy, configured := s.artifactProxy(attempt.ProviderID)
	if !configured {
		return ArtifactRead{}, ErrArtifactProxyRequired
	}
	request := ArtifactProxyRequest{
		ArtifactID: artifact.ID, OperationID: artifact.OperationID, JobID: artifact.JobID, AttemptID: artifact.AttemptID,
		ProviderID: attempt.ProviderID, ProviderAccountID: attempt.ProviderAccountID, ProviderTaskID: attempt.ProviderTaskID,
		ProviderRequestID: attempt.ProviderRequestID, ProviderReference: artifact.ExternalReference, Role: artifact.Role,
		MediaType: artifact.MediaType, ExpectedSizeBytes: artifact.SizeBytes, ExpectedSHA256: artifact.SHA256,
		Owner: artifactOwnerFromJob(job),
	}
	opened, err := proxy.OpenArtifact(ctx, request, byteRange)
	if err != nil {
		return ArtifactRead{}, err
	}
	if err := validateArtifactProxyRead(artifact, byteRange, opened); err != nil {
		if opened.Body != nil {
			_ = opened.Body.Close()
		}
		return ArtifactRead{}, err
	}
	opened.Body = &limitedReadCloser{Reader: io.LimitReader(opened.Body, opened.SizeBytes), closer: opened.Body}
	return opened, nil
}

func artifactOwnerFromOperationLikeArtifact(artifact Artifact) AIJobOwner {
	return AIJobOwner{
		ProfileScope: artifact.ProfileScope, TenantID: artifact.TenantID, IntegrationID: artifact.IntegrationID,
		PrincipalType: artifact.PrincipalType, PrincipalID: artifact.PrincipalID, ExternalSubjectReference: artifact.ExternalSubjectReference,
	}
}

func validateArtifactProxyRead(artifact Artifact, requested *ArtifactByteRange, opened ArtifactRead) error {
	if opened.Body == nil || opened.TotalBytes < 0 || opened.Offset < 0 || opened.SizeBytes < 0 {
		return ErrArtifactIntegrity
	}
	if artifact.SizeBytes > 0 && opened.TotalBytes != artifact.SizeBytes {
		return ErrArtifactIntegrity
	}
	expectedOffset, expectedLength, err := normalizeArtifactByteRange(opened.TotalBytes, requested)
	if err != nil || opened.Offset != expectedOffset || opened.SizeBytes != expectedLength {
		return ErrArtifactIntegrity
	}
	return nil
}
