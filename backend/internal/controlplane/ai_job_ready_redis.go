package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

const redisAIJobReadyMaxPayload = 8 << 10

type RedisAIJobReadyIndexConfig struct {
	Namespace string
}

type RedisAIJobReadyIndex struct {
	client        redis.UniversalClient
	entriesKey    string
	ownersKey     string
	allKey        string
	principalsKey string
	prefix        string
	maxPayload    int
}

var _ AIJobReadyIndex = (*RedisAIJobReadyIndex)(nil)

func NewRedisAIJobReadyIndex(client redis.UniversalClient, config RedisAIJobReadyIndexConfig) (*RedisAIJobReadyIndex, error) {
	if client == nil {
		return nil, ErrAIJobReadyIndexConfig
	}
	namespace := strings.TrimSpace(config.Namespace)
	if namespace == "" {
		namespace = "asterrouter"
	}
	if !validRedisAIJobDeliveryName(namespace) {
		return nil, fmt.Errorf("%w: invalid namespace", ErrAIJobReadyIndexConfig)
	}
	prefix := "aster:{" + namespace + "}:job-ready"
	return &RedisAIJobReadyIndex{
		client: client, entriesKey: prefix + ":entries", ownersKey: prefix + ":owners",
		allKey: prefix + ":all", principalsKey: prefix + ":principals", prefix: prefix,
		maxPayload: redisAIJobReadyMaxPayload,
	}, nil
}

func (index *RedisAIJobReadyIndex) Register(ctx context.Context, entry AIJobReadyEntry) error {
	normalizeAIJobReadyEntry(&entry)
	if err := validateAIJobReadyEntry(entry); err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if len(payload) > index.maxPayload {
		return fmt.Errorf("%w: ready entry exceeds %d bytes", ErrAIJobReadyIndexConfig, index.maxPayload)
	}
	profileDigest, tenantDigest, principalDigest := redisAIJobReadyScopeDigests(entry)
	result, err := redisAIJobReadyRegisterScript.Run(ctx, index.client,
		[]string{index.entriesKey, index.ownersKey, index.allKey, index.principalsKey},
		entry.JobID, entry.StatusVersion, string(payload), entry.ReadyAt.UnixMilli(), profileDigest, tenantDigest, principalDigest, index.prefix,
	).Int()
	if err != nil {
		return err
	}
	if result < 0 {
		return ErrAIJobReadyIndexConflict
	}
	return nil
}

func (index *RedisAIJobReadyIndex) Remove(ctx context.Context, reference AIJobReadyReference) error {
	if strings.TrimSpace(reference.JobID) == "" || reference.StatusVersion <= 0 {
		return ErrAIJobReadyIndexConfig
	}
	return index.remove(ctx, reference.JobID, reference.StatusVersion, false)
}

func (index *RedisAIJobReadyIndex) remove(ctx context.Context, jobID string, statusVersion int, force bool) error {
	forceValue := 0
	if force {
		forceValue = 1
	}
	return redisAIJobReadyRemoveScript.Run(ctx, index.client,
		[]string{index.entriesKey, index.ownersKey, index.allKey, index.principalsKey},
		strings.TrimSpace(jobID), statusVersion, forceValue, index.prefix,
	).Err()
}

func (index *RedisAIJobReadyIndex) Candidates(ctx context.Context, query AIJobReadyQuery) ([]AIJobReadyEntry, error) {
	if query.ReadyAt.IsZero() || query.Limit <= 0 {
		return []AIJobReadyEntry{}, nil
	}
	values, err := redisAIJobReadyCandidatesScript.Run(ctx, index.client,
		[]string{index.entriesKey, index.ownersKey, index.allKey, index.principalsKey},
		query.ReadyAt.UnixMilli(), query.Limit, index.prefix,
	).StringSlice()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	out := make([]AIJobReadyEntry, 0, len(values)/2)
	for offset := 0; offset+1 < len(values); offset += 2 {
		jobID, payload := values[offset], values[offset+1]
		if len(payload) > index.maxPayload {
			if removeErr := index.remove(ctx, jobID, 0, true); removeErr != nil {
				return nil, removeErr
			}
			continue
		}
		var entry AIJobReadyEntry
		decodeErr := json.Unmarshal([]byte(payload), &entry)
		normalizeAIJobReadyEntry(&entry)
		if decodeErr != nil || entry.JobID != jobID || validateAIJobReadyEntry(entry) != nil {
			if removeErr := index.remove(ctx, jobID, 0, true); removeErr != nil {
				return nil, removeErr
			}
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func (index *RedisAIJobReadyIndex) Count(ctx context.Context, scope AIJobReadyScope) (int64, error) {
	if err := validateAIJobReadyScope(scope); err != nil {
		return 0, err
	}
	key := index.allKey
	if scope.Level != AIJobReadyScopeAll {
		key = index.scopeKey(hashAPIKey(aiJobReadyScopeKey(scope)))
	}
	return index.client.ZCard(ctx, key).Result()
}

func (index *RedisAIJobReadyIndex) scopeKey(digest string) string {
	return index.prefix + ":scope:" + digest
}

func redisAIJobReadyScopeDigests(entry AIJobReadyEntry) (string, string, string) {
	return hashAPIKey(aiJobReadyScopeKey(aiJobReadyScopeForEntry(AIJobReadyScopeProfile, entry))),
		hashAPIKey(aiJobReadyScopeKey(aiJobReadyScopeForEntry(AIJobReadyScopeTenant, entry))),
		hashAPIKey(aiJobReadyScopeKey(aiJobReadyScopeForEntry(AIJobReadyScopePrincipal, entry)))
}

var redisAIJobReadyRegisterScript = redis.NewScript(`
local function scope_key(prefix, digest)
  return prefix .. ':scope:' .. digest
end
local function split_owner(owner)
  local parts = {}
  for part in string.gmatch(owner or '', '([^|]+)') do
    table.insert(parts, part)
  end
  return parts
end
local function refresh_principal(prefix, principals_key, digest)
  if not digest or digest == '' then return end
  local key = scope_key(prefix, digest)
  local head = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  if #head == 0 then
    redis.call('ZREM', principals_key, digest)
  else
    redis.call('ZADD', principals_key, head[2], digest)
  end
end
local existing = redis.call('HGET', KEYS[1], ARGV[1])
if existing then
  local ok, decoded = pcall(cjson.decode, existing)
  local existing_version = nil
  if ok then existing_version = tonumber(decoded.status_version) end
  if existing_version and existing_version > tonumber(ARGV[2]) then
    return 0
  end
  if existing_version and existing_version == tonumber(ARGV[2]) and existing ~= ARGV[3] then
    return -1
  end
  local old_owner = split_owner(redis.call('HGET', KEYS[2], ARGV[1]))
  redis.call('ZREM', KEYS[3], ARGV[1])
  for _, digest in ipairs(old_owner) do
    redis.call('ZREM', scope_key(ARGV[8], digest), ARGV[1])
  end
  refresh_principal(ARGV[8], KEYS[4], old_owner[3])
end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[3])
redis.call('HSET', KEYS[2], ARGV[1], ARGV[5] .. '|' .. ARGV[6] .. '|' .. ARGV[7])
redis.call('ZADD', KEYS[3], ARGV[4], ARGV[1])
redis.call('ZADD', scope_key(ARGV[8], ARGV[5]), ARGV[4], ARGV[1])
redis.call('ZADD', scope_key(ARGV[8], ARGV[6]), ARGV[4], ARGV[1])
redis.call('ZADD', scope_key(ARGV[8], ARGV[7]), ARGV[4], ARGV[1])
refresh_principal(ARGV[8], KEYS[4], ARGV[7])
return 1
`)

var redisAIJobReadyRemoveScript = redis.NewScript(`
local function scope_key(prefix, digest)
  return prefix .. ':scope:' .. digest
end
local function split_owner(owner)
  local parts = {}
  for part in string.gmatch(owner or '', '([^|]+)') do
    table.insert(parts, part)
  end
  return parts
end
local function refresh_principal(prefix, principals_key, digest)
  if not digest or digest == '' then return end
  local key = scope_key(prefix, digest)
  local head = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  if #head == 0 then
    redis.call('ZREM', principals_key, digest)
  else
    redis.call('ZADD', principals_key, head[2], digest)
  end
end
local existing = redis.call('HGET', KEYS[1], ARGV[1])
if existing and tonumber(ARGV[3]) ~= 1 then
  local ok, decoded = pcall(cjson.decode, existing)
  local existing_version = nil
  if ok then existing_version = tonumber(decoded.status_version) end
  if not existing_version or existing_version ~= tonumber(ARGV[2]) then
    return 0
  end
end
local owner = split_owner(redis.call('HGET', KEYS[2], ARGV[1]))
redis.call('HDEL', KEYS[1], ARGV[1])
redis.call('HDEL', KEYS[2], ARGV[1])
redis.call('ZREM', KEYS[3], ARGV[1])
for _, digest in ipairs(owner) do
  redis.call('ZREM', scope_key(ARGV[4], digest), ARGV[1])
end
refresh_principal(ARGV[4], KEYS[4], owner[3])
return 1
`)

var redisAIJobReadyCandidatesScript = redis.NewScript(`
local function scope_key(prefix, digest)
  return prefix .. ':scope:' .. digest
end
local principals = redis.call('ZRANGEBYSCORE', KEYS[4], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
local grouped = {}
for index, digest in ipairs(principals) do
  grouped[index] = redis.call('ZRANGEBYSCORE', scope_key(ARGV[3], digest), '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
end
local output = {}
local round = 1
while #output / 2 < tonumber(ARGV[2]) do
  local added = false
  for index, jobs in ipairs(grouped) do
    local job_id = jobs[round]
    if job_id then
      local payload = redis.call('HGET', KEYS[1], job_id)
      if payload then
        table.insert(output, job_id)
        table.insert(output, payload)
      else
        redis.call('ZREM', scope_key(ARGV[3], principals[index]), job_id)
        redis.call('ZREM', KEYS[3], job_id)
      end
      added = true
      if #output / 2 >= tonumber(ARGV[2]) then break end
    end
  end
  if not added then break end
  round = round + 1
end
for _, digest in ipairs(principals) do
  local head = redis.call('ZRANGE', scope_key(ARGV[3], digest), 0, 0, 'WITHSCORES')
  if #head == 0 then
    redis.call('ZREM', KEYS[4], digest)
  else
    redis.call('ZADD', KEYS[4], head[2], digest)
  end
end
return output
`)

func redisAIJobReadyTestKeyPattern(index *RedisAIJobReadyIndex) string {
	return index.prefix + "*"
}
