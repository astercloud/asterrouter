package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/gin-gonic/gin"
)

const (
	aiJobEventPollInterval   = 250 * time.Millisecond
	aiJobEventReauthInterval = time.Second
	aiJobEventHeartbeat      = 15 * time.Second
	aiJobEventRetryMillis    = 1000
)

type publicAIJobEvent struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Version   int       `json:"version"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type publicAIJobArtifactEvent struct {
	ID        string                 `json:"id"`
	JobID     string                 `json:"job_id"`
	Version   int                    `json:"version"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status"`
	Artifact  publicArtifactResponse `json:"artifact"`
	CreatedAt time.Time              `json:"created_at"`
}

type publicAIJobProgressEvent struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	AttemptID string    `json:"attempt_id"`
	Sequence  int64     `json:"sequence"`
	Type      string    `json:"type"`
	Percent   *int      `json:"percent,omitempty"`
	Stage     string    `json:"stage,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func registerGatewayJobEventRoute(r *gin.Engine, control *controlplane.Service) {
	r.GET("/v1/jobs/:job_id/events", func(c *gin.Context) {
		if control == nil {
			openAIError(c, http.StatusServiceUnavailable, "service_unavailable", "gateway control service is not available")
			return
		}
		credential, err := gatewaycore.ExtractCredential(c.Request, gatewaycore.ProtocolAsterJobs)
		if err != nil {
			writeGatewayError(c, controlplane.ErrGatewayUnauthorized)
			return
		}
		auth, err := control.AuthorizeGatewayCredentialScope(c.Request.Context(), credential, gatewaySourceIP(c.Request), controlplane.GatewayScopeJobsRead)
		if err != nil {
			writeGatewayError(c, err)
			return
		}
		cursor, err := parseAIJobEventCursor(c.GetHeader("Last-Event-ID"))
		if err != nil {
			openAIError(c, http.StatusBadRequest, "invalid_event_cursor", "Last-Event-ID must be a non-negative job event version")
			return
		}
		jobID := strings.TrimSpace(c.Param("job_id"))
		if _, found, err := control.AIJobForAuth(c.Request.Context(), auth, jobID); err != nil {
			writeGatewayError(c, err)
			return
		} else if !found {
			openAIError(c, http.StatusNotFound, "resource_not_found", "ai job not found")
			return
		}
		permit, _, acquired, err := control.TryAcquireGatewayCredentialPermit(c.Request.Context(), auth, 0)
		if err != nil {
			openAIError(c, http.StatusInternalServerError, "server_error", "failed to reserve gateway credential capacity")
			return
		}
		if !acquired {
			writeGatewayError(c, controlplane.ErrGatewayCapacityLimited)
			return
		}
		defer permit.Release()

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache, no-transform")
		c.Header("X-Accel-Buffering", "no")
		c.Header("X-AsterRouter-Job-ID", jobID)
		c.Status(http.StatusOK)
		if _, err := fmt.Fprintf(c.Writer, "retry: %d\n\n", aiJobEventRetryMillis); err != nil {
			return
		}
		c.Writer.Flush()
		streamPublicAIJobEvents(c, control, credential, auth, jobID, cursor)
	})
}

func streamPublicAIJobEvents(c *gin.Context, control *controlplane.Service, credential gatewaycore.CredentialEnvelope, auth gatewaycore.CanonicalAuthContext, jobID string, cursor int) {
	pollTicker := time.NewTicker(aiJobEventPollInterval)
	defer pollTicker.Stop()
	reauthTicker := time.NewTicker(aiJobEventReauthInterval)
	defer reauthTicker.Stop()
	heartbeatTicker := time.NewTicker(aiJobEventHeartbeat)
	defer heartbeatTicker.Stop()
	emittedArtifacts := map[string]struct{}{}
	emittedProgress := map[string]struct{}{}

	for {
		job, found, err := control.AIJobForAuth(c.Request.Context(), auth, jobID)
		if err != nil || !found {
			return
		}
		events, err := control.AIJobEvents(c.Request.Context(), jobID)
		if err != nil {
			return
		}
		for _, event := range events {
			if event.Version <= cursor {
				continue
			}
			if err := writePublicAIJobEvent(c, event); err != nil {
				return
			}
			cursor = event.Version
		}
		progressEvents, found, err := control.AIJobProgressEventsForAuth(c.Request.Context(), auth, jobID)
		if err != nil || !found {
			return
		}
		for _, event := range progressEvents {
			if _, emitted := emittedProgress[event.ID]; emitted {
				continue
			}
			if err := writePublicAIJobProgressEvent(c, event); err != nil {
				return
			}
			emittedProgress[event.ID] = struct{}{}
		}
		artifacts, found, err := control.ArtifactsForJobAndAuth(c.Request.Context(), auth, jobID)
		if err != nil || !found {
			return
		}
		for _, artifact := range artifacts {
			if !artifactAvailableForJobEvent(artifact) {
				continue
			}
			if _, emitted := emittedArtifacts[artifact.ID]; emitted {
				continue
			}
			if err := writePublicAIJobArtifactEvent(c, artifact); err != nil {
				return
			}
			emittedArtifacts[artifact.ID] = struct{}{}
		}
		if aiJobPublicTerminal(job.Status) && cursor >= job.StatusVersion {
			return
		}

		select {
		case <-c.Request.Context().Done():
			return
		case <-reauthTicker.C:
			refreshed, err := control.RevalidateGatewayCredentialScope(c.Request.Context(), credential, gatewaySourceIP(c.Request), controlplane.GatewayScopeJobsRead)
			if err != nil {
				return
			}
			if _, found, err := control.AIJobForAuth(c.Request.Context(), refreshed, jobID); err != nil || !found {
				return
			}
			auth = refreshed
		case <-heartbeatTicker.C:
			if _, err := fmt.Fprint(c.Writer, ": keepalive\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case <-pollTicker.C:
		}
	}
}

func writePublicAIJobProgressEvent(c *gin.Context, event controlplane.AIJobProgressEvent) error {
	if event.ID == "" || event.JobID == "" || event.AttemptID == "" || event.ProviderSequence <= 0 || strings.ContainsAny(event.Stage, "\r\n") {
		return errors.New("invalid ai job progress event")
	}
	const eventType = controlplane.AIJobEventProgress
	payload, err := json.Marshal(publicAIJobProgressEvent{
		ID: event.ID, JobID: event.JobID, AttemptID: event.AttemptID, Sequence: event.ProviderSequence,
		Type: eventType, Percent: event.Percent, Stage: event.Stage, CreatedAt: event.CreatedAt,
	})
	if err != nil {
		return err
	}
	// Status events retain the numeric SSE cursor. Progress events carry a
	// stable payload ID and are replayed at least once after reconnect.
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventType, payload); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func artifactAvailableForJobEvent(artifact controlplane.Artifact) bool {
	if artifact.Policy == controlplane.GatewayArtifactPolicyCustomerSink {
		return artifact.Status == controlplane.ArtifactStatusDelivered
	}
	return artifact.Status == controlplane.ArtifactStatusReady || artifact.Status == controlplane.ArtifactStatusDelivered
}

func writePublicAIJobArtifactEvent(c *gin.Context, artifact controlplane.Artifact) error {
	if artifact.ID == "" || artifact.JobID == "" || artifact.StatusVersion <= 0 {
		return errors.New("invalid artifact event")
	}
	const eventType = "job.artifact.available"
	payload, err := json.Marshal(publicAIJobArtifactEvent{
		ID: artifact.ID + ":" + strconv.Itoa(artifact.StatusVersion), JobID: artifact.JobID, Version: artifact.StatusVersion,
		Type: eventType, Status: artifact.Status, Artifact: newPublicArtifactResponse(artifact), CreatedAt: artifact.UpdatedAt,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventType, payload); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func writePublicAIJobEvent(c *gin.Context, event controlplane.AIJobEvent) error {
	if event.Version <= 0 || strings.ContainsAny(event.EventType, "\r\n") {
		return errors.New("invalid ai job event")
	}
	payload, err := json.Marshal(publicAIJobEvent{
		ID: event.ID, JobID: event.JobID, Version: event.Version, Type: event.EventType,
		Status: event.ToStatus, CreatedAt: event.CreatedAt,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Version, event.EventType, payload); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func parseAIJobEventCursor(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.Atoi(value)
	if err != nil || cursor < 0 {
		return 0, errors.New("invalid ai job event cursor")
	}
	return cursor, nil
}
