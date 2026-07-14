package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrTransactionalOutboxPublisherUnavailable = errors.New("transactional outbox publisher is not configured")

const transactionalOutboxLease = 30 * time.Second

// TransactionalOutboxPublisher is an infrastructure boundary. Implementations
// may publish to Redis, a durable broker, or a test transport. They must use the
// stable event ID for downstream deduplication because completion can fail after
// a successful publish, resulting in at-least-once delivery.
type TransactionalOutboxPublisher interface {
	PublishTransactionalOutbox(ctx context.Context, event TransactionalOutboxEvent) error
}

func (s *Service) SetTransactionalOutboxPublisher(publisher TransactionalOutboxPublisher) {
	s.outboxPublisherMu.Lock()
	defer s.outboxPublisherMu.Unlock()
	s.outboxPublisher = publisher
}

func (s *Service) PublishDueTransactionalOutbox(ctx context.Context, limit int) error {
	s.outboxPublisherMu.RLock()
	publisher := s.outboxPublisher
	s.outboxPublisherMu.RUnlock()
	if publisher == nil {
		return ErrTransactionalOutboxPublisherUnavailable
	}
	if limit <= 0 {
		return nil
	}
	now := s.nowUTC()
	leaseToken := "outbox_lease_" + randomID(12)
	events, err := s.repo.ClaimDueTransactionalOutboxEvents(ctx, now, now.Add(transactionalOutboxLease), leaseToken, limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, event := range events {
		publishErr := publisher.PublishTransactionalOutbox(ctx, event)
		if publishErr == nil {
			if err := s.repo.CompleteTransactionalOutboxEvent(ctx, event.ID, leaseToken, s.nowUTC()); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		deadLetter := event.AttemptCount >= event.MaxAttempts
		nextAttemptAt := now
		if !deadLetter {
			nextAttemptAt = now.Add(transactionalOutboxRetryDelay(event.AttemptCount))
		}
		if err := s.repo.RescheduleTransactionalOutboxEvent(ctx, event.ID, leaseToken, nextAttemptAt, publishErr.Error(), deadLetter, s.nowUTC()); err != nil && firstErr == nil {
			firstErr = err
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("publish transactional outbox event %s: %w", event.ID, publishErr)
		}
	}
	return firstErr
}

func (s *Service) RequeueTransactionalOutboxEvent(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("transactional outbox event id is required")
	}
	return s.repo.RequeueTransactionalOutboxEvent(ctx, id, s.nowUTC())
}

func (s *Service) RunTransactionalOutboxScheduler(ctx context.Context, interval time.Duration, batchSize int, onError func(error)) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.PublishDueTransactionalOutbox(ctx, batchSize); err != nil && !errors.Is(err, ErrTransactionalOutboxPublisherUnavailable) && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func transactionalOutboxRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	delay := 5 * time.Second * time.Duration(1<<shift)
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}
