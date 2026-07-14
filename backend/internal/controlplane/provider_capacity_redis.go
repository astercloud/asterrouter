package controlplane

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const maxProviderCapacityLeaseDuration = 7 * 24 * time.Hour

type RedisProviderCapacityStoreConfig struct {
	Namespace string
}

type RedisProviderCapacityStore struct {
	client    redis.UniversalClient
	prefix    string
	ownersKey string
}

var _ ProviderCapacityStore = (*RedisProviderCapacityStore)(nil)

func NewRedisProviderCapacityStore(client redis.UniversalClient, config RedisProviderCapacityStoreConfig) (*RedisProviderCapacityStore, error) {
	if client == nil {
		return nil, ErrProviderCapacityConfig
	}
	namespace := strings.TrimSpace(config.Namespace)
	if namespace == "" {
		namespace = "asterrouter"
	}
	if !validRedisAIJobDeliveryName(namespace) {
		return nil, fmt.Errorf("%w: invalid namespace", ErrProviderCapacityConfig)
	}
	prefix := "aster:{" + namespace + "}:provider-capacity"
	return &RedisProviderCapacityStore{client: client, prefix: prefix, ownersKey: prefix + ":owners"}, nil
}

func (store *RedisProviderCapacityStore) Acquire(ctx context.Context, request ProviderCapacityRequest) (ProviderCapacityLease, string, bool, error) {
	if err := validateProviderCapacityRequest(request); err != nil || request.LeaseDuration > maxProviderCapacityLeaseDuration {
		return ProviderCapacityLease{}, "", false, ErrProviderCapacityConfig
	}
	request.LeaseID = strings.TrimSpace(request.LeaseID)
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	digest := hashAPIKey(request.ProviderAccountID)
	consumeRate := 0
	if request.RPMLimit > 0 || request.TPMLimit > 0 {
		consumeRate = 1
	}
	values, err := redisProviderCapacityAcquireScript.Run(ctx, store.client, store.accountKeys(digest),
		request.LeaseID, digest, request.CapacityUnits, request.ConcurrencyLimit, request.RPMLimit, request.TPMLimit,
		request.EstimatedTokens, request.LeaseDuration.Milliseconds(), consumeRate,
	).Slice()
	if err != nil {
		return ProviderCapacityLease{}, "", false, err
	}
	if len(values) < 2 {
		return ProviderCapacityLease{}, "", false, ErrProviderCapacityConflict
	}
	status, parseErr := strconv.Atoi(redisAIJobDeliveryString(values[0]))
	if parseErr != nil {
		return ProviderCapacityLease{}, "", false, parseErr
	}
	if status < 0 {
		return ProviderCapacityLease{}, "", false, ErrProviderCapacityConflict
	}
	if status == 0 {
		return ProviderCapacityLease{}, redisAIJobDeliveryString(values[1]), false, nil
	}
	expiresAt, parseErr := redisProviderCapacityExpiry(values[1])
	if parseErr != nil {
		return ProviderCapacityLease{}, "", false, parseErr
	}
	return ProviderCapacityLease{
		ID: request.LeaseID, ProviderAccountID: request.ProviderAccountID, CapacityUnits: request.CapacityUnits, ExpiresAt: expiresAt,
	}, "", true, nil
}

func (store *RedisProviderCapacityStore) Extend(ctx context.Context, lease ProviderCapacityLease, duration time.Duration) (ProviderCapacityLease, bool, error) {
	if err := validateProviderCapacityLease(lease); err != nil || duration <= 0 || duration > maxProviderCapacityLeaseDuration {
		return ProviderCapacityLease{}, false, ErrProviderCapacityConfig
	}
	digest := hashAPIKey(strings.TrimSpace(lease.ProviderAccountID))
	values, err := redisProviderCapacityExtendScript.Run(ctx, store.client, store.accountKeys(digest),
		strings.TrimSpace(lease.ID), digest, lease.CapacityUnits, duration.Milliseconds(),
	).Slice()
	if err != nil {
		return ProviderCapacityLease{}, false, err
	}
	if len(values) < 2 {
		return ProviderCapacityLease{}, false, ErrProviderCapacityConflict
	}
	status, parseErr := strconv.Atoi(redisAIJobDeliveryString(values[0]))
	if parseErr != nil {
		return ProviderCapacityLease{}, false, parseErr
	}
	if status < 0 {
		return ProviderCapacityLease{}, false, ErrProviderCapacityConflict
	}
	if status == 0 {
		return ProviderCapacityLease{}, false, nil
	}
	expiresAt, parseErr := redisProviderCapacityExpiry(values[1])
	if parseErr != nil {
		return ProviderCapacityLease{}, false, parseErr
	}
	lease.ExpiresAt = expiresAt
	return lease, true, nil
}

func (store *RedisProviderCapacityStore) Restore(ctx context.Context, lease ProviderCapacityLease, duration time.Duration) (ProviderCapacityLease, error) {
	if err := validateProviderCapacityLease(lease); err != nil || duration <= 0 || duration > maxProviderCapacityLeaseDuration {
		return ProviderCapacityLease{}, ErrProviderCapacityConfig
	}
	lease.ID = strings.TrimSpace(lease.ID)
	lease.ProviderAccountID = strings.TrimSpace(lease.ProviderAccountID)
	digest := hashAPIKey(lease.ProviderAccountID)
	value, err := redisProviderCapacityRestoreScript.Run(ctx, store.client, store.accountKeys(digest),
		lease.ID, digest, lease.CapacityUnits, duration.Milliseconds(),
	).Int64()
	if err != nil {
		return ProviderCapacityLease{}, err
	}
	if value < 0 {
		return ProviderCapacityLease{}, ErrProviderCapacityConflict
	}
	lease.ExpiresAt = time.UnixMilli(value).UTC()
	return lease, nil
}

func (store *RedisProviderCapacityStore) Release(ctx context.Context, lease ProviderCapacityLease) error {
	if err := validateProviderCapacityLease(lease); err != nil {
		return err
	}
	digest := hashAPIKey(strings.TrimSpace(lease.ProviderAccountID))
	result, err := redisProviderCapacityReleaseScript.Run(ctx, store.client, store.accountKeys(digest),
		strings.TrimSpace(lease.ID), digest, lease.CapacityUnits,
	).Int()
	if err != nil {
		return err
	}
	if result < 0 {
		return ErrProviderCapacityConflict
	}
	return nil
}

func (store *RedisProviderCapacityStore) Snapshot(ctx context.Context, providerAccountID string) (ProviderCapacitySnapshot, error) {
	providerAccountID = strings.TrimSpace(providerAccountID)
	if providerAccountID == "" {
		return ProviderCapacitySnapshot{}, ErrProviderCapacityConfig
	}
	digest := hashAPIKey(providerAccountID)
	values, err := redisProviderCapacitySnapshotScript.Run(ctx, store.client, store.accountKeys(digest)).Int64Slice()
	if err != nil {
		return ProviderCapacitySnapshot{}, err
	}
	if len(values) != 3 {
		return ProviderCapacitySnapshot{}, ErrProviderCapacityConflict
	}
	return ProviderCapacitySnapshot{CapacityUnits: int(values[0]), Requests: int(values[1]), Tokens: int(values[2])}, nil
}

func (store *RedisProviderCapacityStore) accountKeys(digest string) []string {
	base := store.prefix + ":account:" + digest
	return []string{store.ownersKey, base + ":leases", base + ":units", base + ":rate", base + ":tokens"}
}

func redisProviderCapacityExpiry(value any) (time.Time, error) {
	millis, err := strconv.ParseInt(redisAIJobDeliveryString(value), 10, 64)
	if err != nil || millis <= 0 {
		return time.Time{}, ErrProviderCapacityConflict
	}
	return time.UnixMilli(millis).UTC(), nil
}

var redisProviderCapacityAcquireScript = redis.NewScript(`
local function now_ms()
  local value = redis.call('TIME')
  return tonumber(value[1]) * 1000 + math.floor(tonumber(value[2]) / 1000)
end
local function owner_parts(value)
  if not value then return nil, nil end
  local digest, units = string.match(value, '^([^|]+)|([^|]+)$')
  return digest, tonumber(units)
end
local function prune(now)
  local expired = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now)
  for _, lease_id in ipairs(expired) do
    redis.call('HDEL', KEYS[1], lease_id)
    redis.call('HDEL', KEYS[3], lease_id)
  end
  redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
  local expired_rates = redis.call('ZRANGEBYSCORE', KEYS[4], '-inf', now - 60000)
  for _, sample_id in ipairs(expired_rates) do
    redis.call('HDEL', KEYS[5], sample_id)
  end
  redis.call('ZREMRANGEBYSCORE', KEYS[4], '-inf', now - 60000)
end
local now = now_ms()
prune(now)
local owner = redis.call('HGET', KEYS[1], ARGV[1])
if owner then
  local digest, units = owner_parts(owner)
  if digest ~= ARGV[2] or units ~= tonumber(ARGV[3]) then
    return {-1, 'lease_conflict'}
  end
  local current_expiry = tonumber(redis.call('ZSCORE', KEYS[2], ARGV[1]))
  if current_expiry and current_expiry > now then
    local expires_at = math.max(current_expiry, now + tonumber(ARGV[8]))
    redis.call('ZADD', KEYS[2], expires_at, ARGV[1])
    return {1, expires_at}
  end
  redis.call('HDEL', KEYS[1], ARGV[1])
  redis.call('HDEL', KEYS[3], ARGV[1])
end
local capacity = 0
local active = redis.call('ZRANGE', KEYS[2], 0, -1)
for _, lease_id in ipairs(active) do
  capacity = capacity + tonumber(redis.call('HGET', KEYS[3], lease_id) or '0')
end
if tonumber(ARGV[4]) > 0 and capacity + tonumber(ARGV[3]) > tonumber(ARGV[4]) then
  return {0, 'concurrency_exhausted'}
end
local requests = tonumber(redis.call('ZCARD', KEYS[4]))
if tonumber(ARGV[5]) > 0 and requests >= tonumber(ARGV[5]) then
  return {0, 'rpm_exhausted'}
end
local tokens = 0
local samples = redis.call('ZRANGE', KEYS[4], 0, -1)
for _, sample_id in ipairs(samples) do
  tokens = tokens + tonumber(redis.call('HGET', KEYS[5], sample_id) or '0')
end
if tonumber(ARGV[6]) > 0 and tokens + tonumber(ARGV[7]) > tonumber(ARGV[6]) then
  return {0, 'tpm_exhausted'}
end
local expires_at = now + tonumber(ARGV[8])
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2] .. '|' .. ARGV[3])
redis.call('HSET', KEYS[3], ARGV[1], ARGV[3])
redis.call('ZADD', KEYS[2], expires_at, ARGV[1])
if tonumber(ARGV[9]) == 1 and not redis.call('ZSCORE', KEYS[4], ARGV[1]) then
  redis.call('HSET', KEYS[5], ARGV[1], ARGV[7])
  redis.call('ZADD', KEYS[4], now, ARGV[1])
end
return {1, expires_at}
`)

var redisProviderCapacityExtendScript = redis.NewScript(`
local value = redis.call('TIME')
local now = tonumber(value[1]) * 1000 + math.floor(tonumber(value[2]) / 1000)
local owner = redis.call('HGET', KEYS[1], ARGV[1])
if not owner then return {0, 0} end
local digest, units = string.match(owner, '^([^|]+)|([^|]+)$')
if digest ~= ARGV[2] or tonumber(units) ~= tonumber(ARGV[3]) then return {-1, 0} end
local current_expiry = tonumber(redis.call('ZSCORE', KEYS[2], ARGV[1]))
if not current_expiry or current_expiry <= now then
  redis.call('HDEL', KEYS[1], ARGV[1])
  redis.call('HDEL', KEYS[3], ARGV[1])
  redis.call('ZREM', KEYS[2], ARGV[1])
  return {0, 0}
end
local expires_at = math.max(current_expiry, now + tonumber(ARGV[4]))
redis.call('ZADD', KEYS[2], expires_at, ARGV[1])
return {1, expires_at}
`)

var redisProviderCapacityRestoreScript = redis.NewScript(`
local value = redis.call('TIME')
local now = tonumber(value[1]) * 1000 + math.floor(tonumber(value[2]) / 1000)
local owner = redis.call('HGET', KEYS[1], ARGV[1])
if owner then
  local digest, units = string.match(owner, '^([^|]+)|([^|]+)$')
  if digest ~= ARGV[2] or tonumber(units) ~= tonumber(ARGV[3]) then return -1 end
end
local expires_at = now + tonumber(ARGV[4])
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2] .. '|' .. ARGV[3])
redis.call('HSET', KEYS[3], ARGV[1], ARGV[3])
redis.call('ZADD', KEYS[2], expires_at, ARGV[1])
return expires_at
`)

var redisProviderCapacityReleaseScript = redis.NewScript(`
local owner = redis.call('HGET', KEYS[1], ARGV[1])
if not owner then return 0 end
local digest, units = string.match(owner, '^([^|]+)|([^|]+)$')
if digest ~= ARGV[2] or tonumber(units) ~= tonumber(ARGV[3]) then return -1 end
redis.call('HDEL', KEYS[1], ARGV[1])
redis.call('HDEL', KEYS[3], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`)

var redisProviderCapacitySnapshotScript = redis.NewScript(`
local value = redis.call('TIME')
local now = tonumber(value[1]) * 1000 + math.floor(tonumber(value[2]) / 1000)
local expired = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now)
for _, lease_id in ipairs(expired) do
  redis.call('HDEL', KEYS[1], lease_id)
  redis.call('HDEL', KEYS[3], lease_id)
end
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
local expired_rates = redis.call('ZRANGEBYSCORE', KEYS[4], '-inf', now - 60000)
for _, sample_id in ipairs(expired_rates) do redis.call('HDEL', KEYS[5], sample_id) end
redis.call('ZREMRANGEBYSCORE', KEYS[4], '-inf', now - 60000)
local capacity = 0
local active = redis.call('ZRANGE', KEYS[2], 0, -1)
for _, lease_id in ipairs(active) do capacity = capacity + tonumber(redis.call('HGET', KEYS[3], lease_id) or '0') end
local tokens = 0
local samples = redis.call('ZRANGE', KEYS[4], 0, -1)
for _, sample_id in ipairs(samples) do tokens = tokens + tonumber(redis.call('HGET', KEYS[5], sample_id) or '0') end
return {capacity, #samples, tokens}
`)

func redisProviderCapacityTestKeyPattern(store *RedisProviderCapacityStore) string {
	return store.prefix + "*"
}
