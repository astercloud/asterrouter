package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/gatewaycore"
	"github.com/coder/websocket"
)

const (
	realtimeMessageLimit      = 4 << 20
	realtimeMaxSession        = time.Hour
	realtimeIdleTimeout       = 90 * time.Second
	realtimeWriteTimeout      = 30 * time.Second
	realtimeRevalidateEvery   = 30 * time.Second
	realtimeRevalidateTimeout = 5 * time.Second
	realtimeClientMessagesPer = 120
	realtimeEventDedupeLimit  = 4096
)

var (
	errRealtimeTextRequired   = errors.New("realtime events must be JSON text messages")
	errRealtimeInvalidEvent   = errors.New("invalid realtime event")
	errRealtimeRateLimited    = errors.New("realtime client message rate exceeded")
	errRealtimeIdleTimeout    = errors.New("realtime session idle timeout")
	errRealtimeLeaseLost      = errors.New("realtime capacity lease lost")
	errRealtimeQuotaExceeded  = errors.New("realtime token quota exceeded")
	errRealtimeBudgetExceeded = errors.New("realtime budget exceeded")
	errRealtimeRiskBlocked    = errors.New("realtime credential was risk blocked")
	errRealtimeCredentialGone = errors.New("realtime credential is no longer valid")
	errRealtimePolicyRevoked  = errors.New("realtime request is no longer allowed by policy")
	errRealtimeRevalidate     = errors.New("realtime credential revalidation failed")
)

type realtimeRelayConfig struct {
	MessageLimit      int64
	MaxSession        time.Duration
	IdleTimeout       time.Duration
	WriteTimeout      time.Duration
	RevalidateEvery   time.Duration
	RevalidateTimeout time.Duration
	ClientMessagesPS  int
	EventDedupeLimit  int
}

func defaultRealtimeRelayConfig() realtimeRelayConfig {
	return realtimeRelayConfig{
		MessageLimit: realtimeMessageLimit, MaxSession: realtimeMaxSession, IdleTimeout: realtimeIdleTimeout,
		WriteTimeout: realtimeWriteTimeout, RevalidateEvery: realtimeRevalidateEvery, RevalidateTimeout: realtimeRevalidateTimeout,
		ClientMessagesPS: realtimeClientMessagesPer, EventDedupeLimit: realtimeEventDedupeLimit,
	}
}

type realtimeRelayStats struct {
	InputAudioBytes      int64
	OutputAudioBytes     int64
	ClientMessageCount   int64
	ProviderMessageCount int64
	TransferBytes        int64
}

type realtimeRelayStatsStore struct {
	mu    sync.Mutex
	value realtimeRelayStats
}

func (store *realtimeRelayStatsStore) recordClient(messageBytes, audioBytes int64) realtimeRelayStats {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.value.ClientMessageCount++
	store.value.TransferBytes += messageBytes
	store.value.InputAudioBytes += audioBytes
	return store.value
}

func (store *realtimeRelayStatsStore) recordProvider(messageBytes, audioBytes int64) realtimeRelayStats {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.value.ProviderMessageCount++
	store.value.TransferBytes += messageBytes
	store.value.OutputAudioBytes += audioBytes
	return store.value
}

func (store *realtimeRelayStatsStore) snapshot() realtimeRelayStats {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.value
}

type realtimeRelayOutcome struct {
	Stats     realtimeRelayStats
	Normal    bool
	ErrorType string
	Summary   string
	Err       error
}

type realtimeRelayResult struct {
	direction string
	err       error
}

type realtimeUsageCallback func(context.Context, gatewayUsageObservation, realtimeRelayStats) error
type realtimeRevalidationCallback func(context.Context) error

func runRealtimeRelay(parent context.Context, downstream, upstream *websocket.Conn, upstreamModel string, credentialLost, providerLost <-chan error, config realtimeRelayConfig, onUsage realtimeUsageCallback, revalidate realtimeRevalidationCallback) realtimeRelayOutcome {
	if config.MessageLimit <= 0 || config.MaxSession <= 0 || config.IdleTimeout <= 0 || config.WriteTimeout <= 0 || config.ClientMessagesPS <= 0 || config.EventDedupeLimit <= 0 ||
		(revalidate != nil && (config.RevalidateEvery <= 0 || config.RevalidateTimeout <= 0)) {
		return realtimeRelayOutcome{ErrorType: "relay_configuration_error", Err: errors.New("invalid realtime relay configuration")}
	}
	downstream.SetReadLimit(config.MessageLimit)
	upstream.SetReadLimit(config.MessageLimit)
	ctx, cancel := context.WithCancel(parent)
	activity := make(chan struct{}, 1)
	results := make(chan realtimeRelayResult, 2)
	stats := &realtimeRelayStatsStore{}
	dedupe := newRealtimeEventDedupe(config.EventDedupeLimit)
	limiter := realtimeMessageRateLimiter{limit: config.ClientMessagesPS}
	finish := func(outcome realtimeRelayOutcome, received int) realtimeRelayOutcome {
		cancel()
		deadline := time.NewTimer(config.WriteTimeout)
		defer deadline.Stop()
		for received < 2 {
			select {
			case <-results:
				received++
			case <-deadline.C:
				outcome.Normal = false
				outcome.ErrorType = "relay_shutdown_timeout"
				outcome.Summary = "realtime relay did not stop after cancellation"
				outcome.Err = errors.New(outcome.Summary)
				outcome.Stats = stats.snapshot()
				return outcome
			}
		}
		outcome.Stats = stats.snapshot()
		return outcome
	}

	go func() {
		results <- realtimeRelayResult{direction: "client", err: relayRealtimeClientEvents(ctx, downstream, upstream, upstreamModel, config, activity, stats, dedupe, &limiter)}
	}()
	go func() {
		results <- realtimeRelayResult{direction: "provider", err: relayRealtimeProviderEvents(ctx, upstream, downstream, config, activity, stats, onUsage, revalidate)}
	}()

	maxTimer := time.NewTimer(config.MaxSession)
	defer maxTimer.Stop()
	idleTimer := time.NewTimer(config.IdleTimeout)
	defer idleTimer.Stop()
	var revalidateTicker *time.Ticker
	var revalidateC <-chan time.Time
	if revalidate != nil {
		revalidateTicker = time.NewTicker(config.RevalidateEvery)
		revalidateC = revalidateTicker.C
		defer revalidateTicker.Stop()
	}
	for {
		select {
		case <-activity:
			resetRealtimeTimer(idleTimer, config.IdleTimeout)
		case err := <-credentialLost:
			return finish(realtimeRelayOutcome{ErrorType: "credential_capacity_lease_lost", Summary: errRealtimeLeaseLost.Error(), Err: errors.Join(errRealtimeLeaseLost, err)}, 0)
		case err := <-providerLost:
			return finish(realtimeRelayOutcome{ErrorType: "provider_capacity_lease_lost", Summary: errRealtimeLeaseLost.Error(), Err: errors.Join(errRealtimeLeaseLost, err)}, 0)
		case <-idleTimer.C:
			return finish(realtimeRelayOutcome{ErrorType: "idle_timeout", Summary: errRealtimeIdleTimeout.Error(), Err: errRealtimeIdleTimeout}, 0)
		case <-maxTimer.C:
			return finish(realtimeRelayOutcome{Normal: true, Summary: "maximum realtime session duration reached"}, 0)
		case <-revalidateC:
			if err := runRealtimeRevalidation(ctx, config.RevalidateTimeout, revalidate); err != nil {
				return finish(classifyRealtimeRelayResult(realtimeRelayResult{direction: "policy", err: err}), 0)
			}
		case result := <-results:
			outcome := classifyRealtimeRelayResult(result)
			if result.direction == "provider" {
				if outcome.Normal {
					_ = downstream.Close(websocket.StatusNormalClosure, "provider closed session")
				} else {
					_ = downstream.Close(realtimeCloseStatus(outcome.ErrorType), "realtime session terminated")
					_ = upstream.Close(websocket.StatusGoingAway, "relay terminated")
				}
			} else if outcome.Normal {
				_ = upstream.Close(websocket.StatusNormalClosure, "client closed session")
			}
			return finish(outcome, 1)
		case <-ctx.Done():
			return finish(realtimeRelayOutcome{ErrorType: "session_canceled", Summary: ctx.Err().Error(), Err: ctx.Err()}, 0)
		}
	}
}

func relayRealtimeClientEvents(ctx context.Context, source, target *websocket.Conn, upstreamModel string, config realtimeRelayConfig, activity chan<- struct{}, stats *realtimeRelayStatsStore, dedupe *realtimeEventDedupe, limiter *realtimeMessageRateLimiter) error {
	for {
		messageType, payload, err := source.Read(ctx)
		if err != nil {
			return err
		}
		touchRealtimeActivity(activity)
		if messageType != websocket.MessageText {
			return errRealtimeTextRequired
		}
		if !limiter.allow(time.Now()) {
			return errRealtimeRateLimited
		}
		forwarded, audioBytes, duplicate, err := rewriteRealtimeClientEvent(payload, upstreamModel, dedupe)
		stats.recordClient(int64(len(payload)), audioBytes)
		if err != nil {
			return err
		}
		if duplicate {
			continue
		}
		writeCtx, cancel := context.WithTimeout(ctx, config.WriteTimeout)
		err = target.Write(writeCtx, websocket.MessageText, forwarded)
		cancel()
		if err != nil {
			return err
		}
	}
}

func relayRealtimeProviderEvents(ctx context.Context, source, target *websocket.Conn, config realtimeRelayConfig, activity chan<- struct{}, stats *realtimeRelayStatsStore, onUsage realtimeUsageCallback, revalidate realtimeRevalidationCallback) error {
	for {
		messageType, payload, err := source.Read(ctx)
		if err != nil {
			return err
		}
		touchRealtimeActivity(activity)
		if messageType != websocket.MessageText {
			return errRealtimeTextRequired
		}
		eventType, response, audioBytes, err := inspectRealtimeProviderEvent(payload)
		current := stats.recordProvider(int64(len(payload)), audioBytes)
		if err != nil {
			return err
		}
		if eventType == "response.done" {
			if onUsage != nil {
				if err := onUsage(ctx, gatewaycore.NormalizeUsage(response), current); err != nil {
					return fmt.Errorf("record realtime usage: %w", err)
				}
			}
			if err := runRealtimeRevalidation(ctx, config.RevalidateTimeout, revalidate); err != nil {
				return fmt.Errorf("revalidate realtime session: %w", err)
			}
		}
		writeCtx, cancel := context.WithTimeout(ctx, config.WriteTimeout)
		err = target.Write(writeCtx, websocket.MessageText, payload)
		cancel()
		if err != nil {
			return err
		}
	}
}

func runRealtimeRevalidation(ctx context.Context, timeout time.Duration, revalidate realtimeRevalidationCallback) error {
	if revalidate == nil {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return revalidate(checkCtx)
}

func rewriteRealtimeClientEvent(payload []byte, upstreamModel string, dedupe *realtimeEventDedupe) ([]byte, int64, bool, error) {
	var event map[string]any
	if len(payload) == 0 || json.Unmarshal(payload, &event) != nil || event == nil {
		return nil, 0, false, errRealtimeInvalidEvent
	}
	eventType, _ := event["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" || len(eventType) > 128 {
		return nil, 0, false, errRealtimeInvalidEvent
	}
	if eventID, _ := event["event_id"].(string); strings.TrimSpace(eventID) != "" {
		eventID = strings.TrimSpace(eventID)
		if len(eventID) > 256 {
			return nil, 0, false, errRealtimeInvalidEvent
		}
		if dedupe.seen(eventID) {
			return nil, 0, true, nil
		}
	}
	switch eventType {
	case "session.update":
		session, ok := event["session"].(map[string]any)
		if !ok {
			session = map[string]any{}
			event["session"] = session
		}
		session["model"] = upstreamModel
	case "response.create":
		response, ok := event["response"].(map[string]any)
		if !ok {
			response = map[string]any{}
			event["response"] = response
		}
		response["model"] = upstreamModel
	}
	audioBytes := int64(0)
	if eventType == "input_audio_buffer.append" {
		audio, ok := event["audio"].(string)
		if !ok || strings.TrimSpace(audio) == "" {
			return nil, 0, false, errRealtimeInvalidEvent
		}
		decoded, err := base64.StdEncoding.DecodeString(audio)
		if err != nil {
			return nil, 0, false, errRealtimeInvalidEvent
		}
		audioBytes = int64(len(decoded))
	}
	forwarded, err := json.Marshal(event)
	if err != nil {
		return nil, 0, false, errRealtimeInvalidEvent
	}
	return forwarded, audioBytes, false, nil
}

func inspectRealtimeProviderEvent(payload []byte) (string, []byte, int64, error) {
	var event struct {
		Type     string          `json:"type"`
		Delta    string          `json:"delta"`
		Response json.RawMessage `json:"response"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &event) != nil || strings.TrimSpace(event.Type) == "" {
		return "", nil, 0, errRealtimeInvalidEvent
	}
	audioBytes := int64(0)
	if strings.Contains(event.Type, "audio.delta") {
		if strings.TrimSpace(event.Delta) == "" {
			return "", nil, 0, errRealtimeInvalidEvent
		}
		decoded, err := base64.StdEncoding.DecodeString(event.Delta)
		if err != nil {
			return "", nil, 0, errRealtimeInvalidEvent
		}
		audioBytes = int64(len(decoded))
	}
	if event.Type == "response.done" {
		if len(event.Response) == 0 || string(event.Response) == "null" {
			event.Response = []byte(`{}`)
		}
	}
	return event.Type, event.Response, audioBytes, nil
}

type realtimeEventDedupe struct {
	limit int
	ids   map[string]struct{}
	order []string
}

func newRealtimeEventDedupe(limit int) *realtimeEventDedupe {
	return &realtimeEventDedupe{limit: limit, ids: make(map[string]struct{}, limit), order: make([]string, 0, limit)}
}

func (dedupe *realtimeEventDedupe) seen(id string) bool {
	if _, found := dedupe.ids[id]; found {
		return true
	}
	if len(dedupe.order) == dedupe.limit {
		delete(dedupe.ids, dedupe.order[0])
		copy(dedupe.order, dedupe.order[1:])
		dedupe.order = dedupe.order[:len(dedupe.order)-1]
	}
	dedupe.ids[id] = struct{}{}
	dedupe.order = append(dedupe.order, id)
	return false
}

type realtimeMessageRateLimiter struct {
	limit       int
	windowStart time.Time
	count       int
}

func (limiter *realtimeMessageRateLimiter) allow(now time.Time) bool {
	if limiter.windowStart.IsZero() || now.Sub(limiter.windowStart) >= time.Second {
		limiter.windowStart = now
		limiter.count = 0
	}
	limiter.count++
	return limiter.count <= limiter.limit
}

func classifyRealtimeRelayResult(result realtimeRelayResult) realtimeRelayOutcome {
	status := websocket.CloseStatus(result.err)
	if status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway {
		return realtimeRelayOutcome{Normal: true, Summary: result.direction + " closed the realtime session"}
	}
	errorType := result.direction + "_connection_error"
	switch {
	case errors.Is(result.err, errRealtimeTextRequired), errors.Is(result.err, errRealtimeInvalidEvent):
		errorType = "protocol_error"
	case errors.Is(result.err, errRealtimeRateLimited):
		errorType = "message_rate_exceeded"
	case errors.Is(result.err, errRealtimeQuotaExceeded):
		errorType = "quota_exceeded"
	case errors.Is(result.err, errRealtimeBudgetExceeded):
		errorType = "budget_exceeded"
	case errors.Is(result.err, errRealtimeRiskBlocked):
		errorType = "risk_blocked"
	case errors.Is(result.err, errRealtimeCredentialGone):
		errorType = "credential_revoked"
	case errors.Is(result.err, errRealtimePolicyRevoked):
		errorType = "policy_revoked"
	case errors.Is(result.err, errRealtimeRevalidate):
		errorType = "credential_revalidation_error"
	case strings.Contains(result.err.Error(), "record realtime usage"):
		errorType = "usage_ledger_error"
	case errors.Is(result.err, websocket.ErrMessageTooBig), status == websocket.StatusMessageTooBig:
		errorType = "message_too_large"
	}
	summary := result.direction + " realtime relay failed"
	if result.err != nil {
		summary = result.err.Error()
	}
	return realtimeRelayOutcome{ErrorType: errorType, Summary: summary, Err: result.err}
}

func touchRealtimeActivity(activity chan<- struct{}) {
	select {
	case activity <- struct{}{}:
	default:
	}
}

func resetRealtimeTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
