package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"
)

const artifactDeliveryLease = 2 * time.Minute

var (
	ErrArtifactSinkRequired         = errors.New("customer artifact sink is required")
	ErrArtifactSinkForbidden        = errors.New("customer artifact sink does not accept this owner")
	ErrArtifactSinkInvalid          = errors.New("customer artifact sink is invalid")
	ErrArtifactDeliveryInProgress   = errors.New("artifact delivery is already in progress")
	ErrArtifactSinkReferenceInvalid = errors.New("customer artifact sink returned an invalid reference")
)

type ArtifactSinkRequest struct {
	SinkID            string
	IdempotencyKey    string
	ArtifactID        string
	OperationID       string
	JobID             string
	AttemptID         string
	Role              string
	MediaType         string
	ExpectedSizeBytes int64
	ExpectedSHA256    string
	Owner             ArtifactOwner
}

type ArtifactSinkResult struct {
	ExternalReference string
}

// ArtifactSink is a plugin-owned destination. Implementations must commit
// atomically and treat IdempotencyKey as an overwrite-safe delivery identity.
type ArtifactSink interface {
	ID() string
	Accepts(ArtifactOwner) bool
	DeliverArtifact(context.Context, ArtifactSinkRequest, io.Reader) (ArtifactSinkResult, error)
	DeleteArtifact(context.Context, ArtifactSinkRequest) error
}

func (s *Service) SetArtifactSink(sink ArtifactSink) error {
	if sink == nil || !validArtifactSinkID(sink.ID()) {
		return ErrArtifactSinkInvalid
	}
	s.artifactSinkMu.Lock()
	defer s.artifactSinkMu.Unlock()
	s.artifactSinks[strings.TrimSpace(sink.ID())] = sink
	return nil
}

func (s *Service) RemoveArtifactSink(id string) {
	s.artifactSinkMu.Lock()
	defer s.artifactSinkMu.Unlock()
	delete(s.artifactSinks, strings.TrimSpace(id))
}

func (s *Service) artifactSink(id string) (ArtifactSink, bool) {
	s.artifactSinkMu.RLock()
	defer s.artifactSinkMu.RUnlock()
	sink, found := s.artifactSinks[strings.TrimSpace(id)]
	return sink, found
}

func validArtifactSinkID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 160 {
		return false
	}
	for _, char := range id {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-_.:", char) {
			continue
		}
		return false
	}
	return true
}

func (s *Service) ensureCustomerSinkProviderOutput(
	ctx context.Context,
	provider GatewayProvider,
	job AIJob,
	attempt AIAttempt,
	output ProviderOutputDescriptor,
	input ArtifactCreateInput,
	artifact Artifact,
	found bool,
	adapter DurableAIJobAdapter,
) (Artifact, error) {
	sink, configured := s.artifactSink(job.ArtifactSinkID)
	if !configured {
		return Artifact{}, ErrArtifactSinkRequired
	}
	owner := artifactOwnerFromJob(job)
	if !sink.Accepts(owner) {
		return Artifact{}, ErrArtifactSinkForbidden
	}
	readerAdapter, ok := adapter.(DurableAIJobOutputReader)
	if !ok {
		return Artifact{}, ErrProviderOutputReaderRequired
	}
	reader := &providerOutputReader{ctx: ctx, adapter: readerAdapter, provider: provider, job: job, attempt: attempt, output: output}
	defer reader.Close()
	if !found {
		created, err := s.createProviderOutputArtifact(ctx, input, nil)
		if err != nil {
			if errors.Is(err, ErrArtifactIngestInProgress) {
				return created, ErrArtifactDeliveryInProgress
			}
			return created, err
		}
		artifact = created
	}
	return s.deliverArtifactToCustomerSink(ctx, artifact, input, reader, sink, owner)
}

func artifactOwnerFromJob(job AIJob) ArtifactOwner {
	return ArtifactOwner{
		ProfileScope: job.ProfileScope, TenantID: job.TenantID, IntegrationID: job.IntegrationID,
		PrincipalType: job.PrincipalType, PrincipalID: job.PrincipalID, ExternalSubjectReference: job.ExternalSubjectReference,
	}
}

func (s *Service) deliverArtifactToCustomerSink(ctx context.Context, artifact Artifact, input ArtifactCreateInput, body io.Reader, sink ArtifactSink, owner ArtifactOwner) (Artifact, error) {
	if artifact.Status == ArtifactStatusDelivered {
		return artifact, nil
	}
	if !oneOf(artifact.Status, ArtifactStatusReady, ArtifactStatusDelivering, ArtifactStatusDeliveryFailed) {
		return Artifact{}, ErrArtifactUnavailable
	}
	if artifact.Status == ArtifactStatusDelivering && artifact.UpdatedAt.After(s.nowUTC().Add(-artifactDeliveryLease)) {
		return Artifact{}, ErrArtifactDeliveryInProgress
	}
	delivering, err := s.transitionArtifact(ctx, artifact, ArtifactStatusDelivering, "sink_delivery_claimed", nil)
	if errors.Is(err, ErrArtifactStateConflict) {
		return Artifact{}, ErrArtifactDeliveryInProgress
	}
	if err != nil {
		return Artifact{}, err
	}
	request := ArtifactSinkRequest{
		SinkID: sink.ID(), IdempotencyKey: artifact.ID, ArtifactID: artifact.ID, OperationID: artifact.OperationID,
		JobID: artifact.JobID, AttemptID: artifact.AttemptID, Role: artifact.Role, MediaType: strings.TrimSpace(input.MediaType),
		ExpectedSizeBytes: input.ExpectedSizeBytes, ExpectedSHA256: strings.ToLower(strings.TrimSpace(input.ExpectedSHA256)), Owner: owner,
	}
	return s.writeArtifactToCustomerSink(ctx, delivering, input, request, body, sink)
}

func (s *Service) writeArtifactToCustomerSink(ctx context.Context, delivering Artifact, input ArtifactCreateInput, request ArtifactSinkRequest, body io.Reader, sink ArtifactSink) (Artifact, error) {
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = ArtifactDefaultMaxBytes
	}
	hasher := sha256.New()
	counter := &artifactByteCounter{}
	limited := io.LimitReader(body, maxBytes+1)
	tracked := &artifactCompletionReader{reader: io.TeeReader(limited, io.MultiWriter(hasher, counter))}
	heartbeat := s.startArtifactStatusHeartbeat(ctx, delivering, ArtifactStatusDelivering, "delivery_heartbeat", artifactDeliveryLease)
	result, sinkErr := sink.DeliverArtifact(heartbeat.Context(), request, tracked)
	delivering, heartbeatErr := heartbeat.Stop()
	if heartbeatErr != nil {
		return Artifact{}, errors.Join(sinkErr, heartbeatErr)
	}
	completionErr := error(nil)
	if sinkErr == nil {
		completionErr = tracked.RequireEOF()
	}
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	verificationErr := errors.Join(completionErr, verifyArtifactContent(input, counter.total, actualSHA, maxBytes))
	var referenceErr error
	if sinkErr == nil {
		referenceErr = validateArtifactSinkReference(result.ExternalReference)
	}
	if sinkErr != nil || verificationErr != nil || referenceErr != nil {
		cleanupErr := sink.DeleteArtifact(ctx, request)
		content := &ArtifactContentUpdate{MediaType: request.MediaType, SizeBytes: counter.total, SHA256: actualSHA, StoreDriver: ArtifactStoreDriverNone}
		failed, transitionErr := s.transitionArtifact(ctx, delivering, ArtifactStatusDeliveryFailed, artifactSinkFailureReason(sinkErr, verificationErr, referenceErr, cleanupErr), content)
		return failed, errors.Join(sinkErr, verificationErr, referenceErr, cleanupErr, transitionErr)
	}
	content := &ArtifactContentUpdate{
		MediaType: request.MediaType, SizeBytes: counter.total, SHA256: actualSHA, StoreDriver: ArtifactStoreDriverNone,
		ExternalReference: strings.TrimSpace(result.ExternalReference),
	}
	return s.transitionArtifact(ctx, delivering, ArtifactStatusDelivered, "", content)
}

func validateArtifactSinkReference(reference string) error {
	reference = strings.TrimSpace(reference)
	if reference == "" || len(reference) > 4096 || strings.ContainsAny(reference, "\x00\r\n") {
		return ErrArtifactSinkReferenceInvalid
	}
	return nil
}

func artifactSinkFailureReason(sinkErr, verificationErr, referenceErr, cleanupErr error) string {
	switch {
	case verificationErr != nil:
		return artifactFailureReason(nil, verificationErr)
	case referenceErr != nil:
		return "sink_reference_invalid"
	case cleanupErr != nil:
		return "sink_cleanup_failed"
	case sinkErr != nil:
		return "sink_delivery_failed"
	default:
		return "sink_delivery_failed"
	}
}

type artifactCompletionReader struct {
	reader io.Reader
	eof    bool
}

func (r *artifactCompletionReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		r.eof = true
	}
	return read, err
}

func (r *artifactCompletionReader) RequireEOF() error {
	if r.eof {
		return nil
	}
	var probe [1]byte
	read, err := r.Read(probe[:])
	if read > 0 {
		return ErrArtifactIntegrity
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrArtifactIntegrity
}
