package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
)

var (
	ErrArtifactUploadInvalid    = errors.New("invalid artifact upload session")
	ErrArtifactUploadIncomplete = errors.New("artifact upload is incomplete")
	ErrArtifactUploadOffset     = errors.New("artifact upload offset conflict")
)

type ArtifactUploadState struct {
	ExpectedSize   int64  `json:"expected_size"`
	ExpectedSHA256 string `json:"expected_sha256"`
	Offset         int64  `json:"offset"`
	MediaType      string `json:"media_type"`
	StoreDriver    string `json:"store_driver"`
	Completed      bool   `json:"completed,omitempty"`
}

type artifactUploadChunkState struct {
	Offset    int64  `json:"offset"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
}

func encodeArtifactUploadState(state ArtifactUploadState) (string, error) {
	state.ExpectedSHA256 = strings.ToLower(strings.TrimSpace(state.ExpectedSHA256))
	state.MediaType = strings.TrimSpace(state.MediaType)
	state.StoreDriver = strings.TrimSpace(state.StoreDriver)
	if state.ExpectedSize <= 0 || state.Offset < 0 || state.Offset > state.ExpectedSize || len(state.ExpectedSHA256) != 64 || state.MediaType == "" || state.StoreDriver == "" {
		return "", ErrArtifactUploadInvalid
	}
	if _, err := hex.DecodeString(state.ExpectedSHA256); err != nil {
		return "", ErrArtifactUploadInvalid
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeArtifactUploadState(value string) (ArtifactUploadState, error) {
	var state ArtifactUploadState
	if strings.TrimSpace(value) == "" || json.Unmarshal([]byte(value), &state) != nil {
		return ArtifactUploadState{}, ErrArtifactUploadInvalid
	}
	if _, err := encodeArtifactUploadState(state); err != nil {
		return ArtifactUploadState{}, err
	}
	return state, nil
}

func encodeArtifactUploadChunkState(state artifactUploadChunkState) (string, error) {
	if state.Offset < 0 || state.Size <= 0 || len(state.SHA256) != 64 || strings.TrimSpace(state.MediaType) == "" {
		return "", ErrArtifactUploadInvalid
	}
	if _, err := hex.DecodeString(strings.ToLower(state.SHA256)); err != nil {
		return "", ErrArtifactUploadInvalid
	}
	encoded, err := json.Marshal(state)
	return string(encoded), err
}

func decodeArtifactUploadChunkState(value string) (artifactUploadChunkState, error) {
	var state artifactUploadChunkState
	if strings.TrimSpace(value) == "" || json.Unmarshal([]byte(value), &state) != nil {
		return artifactUploadChunkState{}, ErrArtifactUploadInvalid
	}
	if _, err := encodeArtifactUploadChunkState(state); err != nil {
		return artifactUploadChunkState{}, err
	}
	return state, nil
}

func (s *Service) ArtifactUploadStateForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, id string) (Artifact, ArtifactUploadState, bool, error) {
	artifact, found, err := s.ArtifactForAuth(ctx, auth, id)
	if err != nil || !found {
		return Artifact{}, ArtifactUploadState{}, found, err
	}
	if artifact.Role != ArtifactRoleInput {
		return Artifact{}, ArtifactUploadState{}, false, nil
	}
	state, err := decodeArtifactUploadState(artifact.ExternalReference)
	if err != nil {
		return Artifact{}, ArtifactUploadState{}, true, err
	}
	return artifact, state, true, nil
}

func (s *Service) UpdateArtifactUploadState(ctx context.Context, auth gatewaycore.CanonicalAuthContext, id string, expectedVersion int, state ArtifactUploadState) (Artifact, bool, error) {
	artifact, found, err := s.ArtifactForAuth(ctx, auth, id)
	if err != nil || !found {
		return Artifact{}, false, err
	}
	if artifact.Role != ArtifactRoleInput || !oneOf(artifact.Status, ArtifactStatusPending, ArtifactStatusUploading) {
		return artifact, false, ErrArtifactUploadInvalid
	}
	reference, err := encodeArtifactUploadState(state)
	if err != nil {
		return artifact, false, err
	}
	if expectedVersion <= 0 {
		expectedVersion = artifact.StatusVersion
	}
	updated, changed, err := s.repo.TransitionArtifact(ctx, ArtifactTransitionInput{
		ArtifactID: artifact.ID, ExpectedVersion: expectedVersion, ToStatus: ArtifactStatusUploading,
		Reason: "upload_chunk_committed", Content: &ArtifactContentUpdate{
			MediaType: state.MediaType, SizeBytes: artifact.SizeBytes, StoreDriver: ArtifactStoreDriverNone, ExternalReference: reference,
		},
	}, s.nowUTC())
	return updated, changed, err
}

func (s *Service) ListArtifactUploadChunksForAuth(ctx context.Context, auth gatewaycore.CanonicalAuthContext, sessionID string) ([]Artifact, error) {
	owner := ArtifactOwner(aiJobOwnerFromAuth(auth))
	return s.repo.QueryArtifacts(ctx, ArtifactQuery{Owner: &owner, SourceArtifactID: strings.TrimSpace(sessionID), Role: ArtifactRoleDerived, Status: ArtifactStatusReady, Limit: 100})
}

func (s *Service) CompleteArtifactUpload(ctx context.Context, auth gatewaycore.CanonicalAuthContext, sessionID string, expectedVersion int) (Artifact, error) {
	session, state, found, err := s.ArtifactUploadStateForAuth(ctx, auth, sessionID)
	if err != nil || !found {
		return Artifact{}, ErrArtifactNotFound
	}
	if session.Status == ArtifactStatusReady && state.Completed {
		return session, nil
	}
	if session.Status != ArtifactStatusUploading || state.Offset != state.ExpectedSize {
		return session, ErrArtifactUploadIncomplete
	}
	if expectedVersion <= 0 {
		expectedVersion = session.StatusVersion
	}
	chunks, err := s.ListArtifactUploadChunksForAuth(ctx, auth, session.ID)
	if err != nil {
		return Artifact{}, err
	}
	type uploadChunk struct {
		artifact Artifact
		state    artifactUploadChunkState
	}
	ordered := make([]uploadChunk, 0, len(chunks))
	for _, chunk := range chunks {
		chunkState, decodeErr := decodeArtifactUploadChunkState(chunk.ExternalReference)
		if decodeErr != nil {
			return Artifact{}, decodeErr
		}
		ordered = append(ordered, uploadChunk{artifact: chunk, state: chunkState})
	}
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].state.Offset < ordered[right].state.Offset })
	if len(ordered) == 0 {
		return Artifact{}, ErrArtifactUploadIncomplete
	}
	readers := make([]io.Reader, 0, len(ordered))
	closers := make([]io.Closer, 0, len(ordered))
	position := int64(0)
	for _, chunk := range ordered {
		if chunk.state.Offset != position || chunk.state.Size != chunk.artifact.SizeBytes {
			return Artifact{}, ErrArtifactUploadOffset
		}
		store, ok := s.artifactStore(chunk.artifact.StoreDriver)
		if !ok {
			return Artifact{}, ErrArtifactStoreRequired
		}
		opened, openErr := store.Open(ctx, chunk.artifact.StoreKey, nil)
		if openErr != nil {
			return Artifact{}, openErr
		}
		readers = append(readers, opened.Body)
		closers = append(closers, opened.Body)
		position += chunk.state.Size
	}
	if position != state.ExpectedSize {
		return Artifact{}, ErrArtifactUploadIncomplete
	}
	store, ok := s.artifactStore(state.StoreDriver)
	if !ok {
		return Artifact{}, ErrArtifactStoreRequired
	}
	targetKey := session.ID + "/content"
	hasher := sha256.New()
	counter := &artifactByteCounter{}
	written, putErr := store.Put(ctx, targetKey, io.TeeReader(io.MultiReader(readers...), io.MultiWriter(hasher, counter)), state.ExpectedSize, state.MediaType)
	for _, closer := range closers {
		_ = closer.Close()
	}
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if putErr != nil || written != state.ExpectedSize || counter.total != state.ExpectedSize || actualSHA != state.ExpectedSHA256 {
		_ = store.Delete(ctx, targetKey)
		if putErr != nil {
			return Artifact{}, putErr
		}
		return Artifact{}, ErrArtifactIntegrity
	}
	state.Completed = true
	reference, err := encodeArtifactUploadState(state)
	if err != nil {
		_ = store.Delete(ctx, targetKey)
		return Artifact{}, err
	}
	updated, changed, err := s.repo.TransitionArtifact(ctx, ArtifactTransitionInput{
		ArtifactID: session.ID, ExpectedVersion: expectedVersion, ToStatus: ArtifactStatusReady,
		Reason: "upload_completed", Content: &ArtifactContentUpdate{
			MediaType: state.MediaType, SizeBytes: state.ExpectedSize, SHA256: actualSHA,
			StoreDriver: state.StoreDriver, StoreKey: targetKey, ExternalReference: reference,
		},
	}, s.nowUTC())
	if err != nil {
		_ = store.Delete(ctx, targetKey)
		return Artifact{}, err
	}
	if !changed {
		if updated.Status == ArtifactStatusReady {
			return updated, nil
		}
		_ = store.Delete(ctx, targetKey)
		return updated, ErrArtifactUploadOffset
	}
	for _, chunk := range ordered {
		if chunkStore, exists := s.artifactStore(chunk.artifact.StoreDriver); exists {
			_ = chunkStore.Delete(ctx, chunk.artifact.StoreKey)
		}
		_, _, _ = s.repo.TransitionArtifact(ctx, ArtifactTransitionInput{ArtifactID: chunk.artifact.ID, ExpectedVersion: chunk.artifact.StatusVersion, ToStatus: ArtifactStatusExpired, Reason: "upload_chunk_compacted"}, s.nowUTC())
	}
	return updated, nil
}
