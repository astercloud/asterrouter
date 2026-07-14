package controlplane

import (
	"context"
	"time"
)

// RunPlatformUsageDeliveryScheduler drains durable usage delivery events.
// Delivery never blocks the gateway request that created the Usage record;
// each event is retried through the repository-backed lease state machine.
func (s *Service) RunPlatformUsageDeliveryScheduler(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.DeliverDuePlatformUsage(ctx, 50); err != nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
