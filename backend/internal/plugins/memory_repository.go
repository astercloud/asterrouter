package plugins

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu            sync.RWMutex
	plugins       map[string]Plugin
	configs       map[string]configRecord
	deliveries    map[string]DeliveryAttempt
	snapshots     []catalogSnapshotRecord
	packages      map[string]packageRecord
	advisories    map[string]advisoryRecord
	caches        map[string]packageCacheRecord
	installations map[string]packageInstallationRecord
	licenses      []licenseRecord
	apiTokens     map[string]pluginAPITokenRecord
	feeds         map[string]officialFeedRecord
	feedSyncRuns  []officialFeedSyncRunRecord
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		plugins:       map[string]Plugin{},
		configs:       map[string]configRecord{},
		deliveries:    map[string]DeliveryAttempt{},
		packages:      map[string]packageRecord{},
		advisories:    map[string]advisoryRecord{},
		caches:        map[string]packageCacheRecord{},
		installations: map[string]packageInstallationRecord{},
		licenses:      []licenseRecord{},
		apiTokens:     map[string]pluginAPITokenRecord{},
		feeds:         map[string]officialFeedRecord{},
		feedSyncRuns:  []officialFeedSyncRunRecord{},
	}
}

func (r *MemoryRepository) ListPlugins(context.Context) ([]Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Plugin, 0, len(r.plugins))
	for _, plugin := range r.plugins {
		out = append(out, plugin)
	}
	sortPlugins(out)
	return out, nil
}

func (r *MemoryRepository) SavePlugin(_ context.Context, plugin Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[plugin.ID] = plugin
	return nil
}

func (r *MemoryRepository) FindPlugin(_ context.Context, id string) (Plugin, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plugin, ok := r.plugins[id]
	return plugin, ok, nil
}

func (r *MemoryRepository) UpdateStatus(_ context.Context, id string, status string, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	plugin, ok := r.plugins[id]
	if !ok {
		return nil
	}
	plugin.Status = status
	plugin.UpdatedAt = updatedAt
	r.plugins[id] = plugin
	return nil
}

func (r *MemoryRepository) FindConfig(_ context.Context, pluginID string) (configRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.configs[pluginID]
	if !ok {
		return configRecord{}, false, nil
	}
	return cloneConfigRecord(record), true, nil
}

func (r *MemoryRepository) SaveConfig(_ context.Context, record configRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[record.PluginID] = cloneConfigRecord(record)
	return nil
}

func (r *MemoryRepository) QueryDeliveryAttempts(_ context.Context, query DeliveryQuery) ([]DeliveryAttempt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]DeliveryAttempt, 0, len(r.deliveries))
	for _, attempt := range r.deliveries {
		if !deliveryAttemptMatches(attempt, query) {
			continue
		}
		out = append(out, attempt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit, offset := normalizeListWindow(query.Limit, query.Offset, 50, 500)
	if offset >= len(out) {
		return []DeliveryAttempt{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *MemoryRepository) SaveDeliveryAttempt(_ context.Context, attempt DeliveryAttempt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries[attempt.ID] = attempt
	return nil
}

func (r *MemoryRepository) SaveCatalogSnapshot(_ context.Context, record catalogSnapshotRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots = append(r.snapshots, record)
	return nil
}

func (r *MemoryRepository) LatestCatalogSnapshot(_ context.Context) (catalogSnapshotRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.snapshots) == 0 {
		return catalogSnapshotRecord{}, false, nil
	}
	return r.snapshots[len(r.snapshots)-1], true, nil
}

func (r *MemoryRepository) SavePackage(_ context.Context, record packageRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.packages[record.PackageID] = record
	return nil
}

func (r *MemoryRepository) ListPackages(_ context.Context, pluginID string) ([]packageRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]packageRecord, 0, len(r.packages))
	for _, record := range r.packages {
		if pluginID != "" && record.PluginID != pluginID {
			continue
		}
		out = append(out, record)
	}
	sortPackageRecords(out)
	return out, nil
}

func (r *MemoryRepository) FindPackage(_ context.Context, packageID string) (packageRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.packages[packageID]
	return record, ok, nil
}

func (r *MemoryRepository) SaveAdvisory(_ context.Context, record advisoryRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.advisories[record.PublicID] = cloneAdvisoryRecord(record)
	return nil
}

func (r *MemoryRepository) ListRevokedAffectedVersions(_ context.Context, pluginID string) ([]affectedVersionRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pluginID = strings.TrimSpace(pluginID)
	out := []affectedVersionRecord{}
	for _, advisory := range r.advisories {
		for _, item := range advisory.Affected {
			if !item.Revoked || !affectedVersionMatchesPlugin(item, pluginID) {
				continue
			}
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) SavePackageCache(_ context.Context, record packageCacheRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.caches[record.PackageID] = record
	return nil
}

func (r *MemoryRepository) FindPackageCache(_ context.Context, packageID string) (packageCacheRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.caches[packageID]
	return record, ok, nil
}

func (r *MemoryRepository) SavePackageInstallation(_ context.Context, record packageInstallationRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.installations[record.PluginID] = record
	return nil
}

func (r *MemoryRepository) FindPackageInstallation(_ context.Context, pluginID string) (packageInstallationRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.installations[pluginID]
	return record, ok, nil
}

func (r *MemoryRepository) SaveLicense(_ context.Context, record licenseRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.licenses = append(r.licenses, record)
	return nil
}

func (r *MemoryRepository) LatestLicense(_ context.Context) (licenseRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.licenses) == 0 {
		return licenseRecord{}, false, nil
	}
	return r.licenses[len(r.licenses)-1], true, nil
}

func (r *MemoryRepository) SavePluginAPIToken(_ context.Context, record pluginAPITokenRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.apiTokens[record.ID] = clonePluginAPITokenRecord(record)
	return nil
}

func (r *MemoryRepository) ListPluginAPITokens(_ context.Context, pluginID string) ([]pluginAPITokenRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]pluginAPITokenRecord, 0, len(r.apiTokens))
	for _, record := range r.apiTokens {
		if strings.TrimSpace(pluginID) != "" && record.PluginID != strings.TrimSpace(pluginID) {
			continue
		}
		out = append(out, clonePluginAPITokenRecord(record))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (r *MemoryRepository) FindPluginAPIToken(_ context.Context, tokenHash string) (pluginAPITokenRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, record := range r.apiTokens {
		if record.TokenHash == tokenHash {
			return clonePluginAPITokenRecord(record), true, nil
		}
	}
	return pluginAPITokenRecord{}, false, nil
}

func (r *MemoryRepository) RevokePluginAPIToken(_ context.Context, id string, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.apiTokens[id]
	if !ok {
		return nil
	}
	record.Status = PluginAPITokenRevoked
	record.UpdatedAt = updatedAt
	r.apiTokens[id] = record
	return nil
}

func (r *MemoryRepository) TouchPluginAPIToken(_ context.Context, id string, usedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.apiTokens[id]
	if !ok {
		return nil
	}
	record.LastUsedAt = &usedAt
	record.UpdatedAt = usedAt
	r.apiTokens[id] = record
	return nil
}

func (r *MemoryRepository) SaveOfficialFeed(_ context.Context, record officialFeedRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.feeds[record.ServiceKey+"|"+record.FeedID] = record
	return nil
}

func (r *MemoryRepository) ListOfficialFeeds(_ context.Context, serviceKey string) ([]officialFeedRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []officialFeedRecord{}
	for _, record := range r.feeds {
		if strings.TrimSpace(serviceKey) != "" && record.ServiceKey != strings.TrimSpace(serviceKey) {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImportedAt.After(out[j].ImportedAt) })
	return out, nil
}

func (r *MemoryRepository) LatestOfficialFeed(ctx context.Context, serviceKey string) (officialFeedRecord, bool, error) {
	items, err := r.ListOfficialFeeds(ctx, serviceKey)
	if err != nil || len(items) == 0 {
		return officialFeedRecord{}, false, err
	}
	return items[0], true, nil
}

func (r *MemoryRepository) UpdateOfficialFeedStatus(_ context.Context, serviceKey string, feedID string, status string, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.TrimSpace(serviceKey) + "|" + strings.TrimSpace(feedID)
	record, ok := r.feeds[key]
	if !ok {
		return nil
	}
	record.Status = strings.TrimSpace(status)
	record.UpdatedAt = updatedAt
	r.feeds[key] = record
	return nil
}

func (r *MemoryRepository) SaveOfficialFeedSyncRun(_ context.Context, record officialFeedSyncRunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.feedSyncRuns {
		if r.feedSyncRuns[index].ID == record.ID {
			r.feedSyncRuns[index] = record
			return nil
		}
	}
	r.feedSyncRuns = append(r.feedSyncRuns, record)
	return nil
}

func (r *MemoryRepository) ListOfficialFeedSyncRuns(_ context.Context, serviceKey string, limit int) ([]officialFeedSyncRunRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	serviceKey = strings.TrimSpace(serviceKey)
	out := make([]officialFeedSyncRunRecord, 0, len(r.feedSyncRuns))
	for _, record := range r.feedSyncRuns {
		if serviceKey != "" && record.ServiceKey != serviceKey {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepository) Health(context.Context) error {
	return nil
}

func (r *MemoryRepository) Close() error {
	return nil
}

func clonePluginAPITokenRecord(record pluginAPITokenRecord) pluginAPITokenRecord {
	clone := record
	clone.Scopes = append([]string(nil), record.Scopes...)
	clone.Surfaces = append([]string(nil), record.Surfaces...)
	if record.ExpiresAt != nil {
		value := *record.ExpiresAt
		clone.ExpiresAt = &value
	}
	if record.LastUsedAt != nil {
		value := *record.LastUsedAt
		clone.LastUsedAt = &value
	}
	return clone
}
