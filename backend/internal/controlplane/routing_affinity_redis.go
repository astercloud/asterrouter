package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisRoutingAffinityDefaultNamespace = "asterrouter"

type RedisRoutingAffinityCoordinatorConfig struct {
	Namespace string
}

type RedisRoutingAffinityCoordinator struct {
	client    redis.UniversalClient
	keyPrefix string
}

var _ RoutingAffinityCoordinator = (*RedisRoutingAffinityCoordinator)(nil)

func NewRedisRoutingAffinityCoordinator(client redis.UniversalClient, config RedisRoutingAffinityCoordinatorConfig) (*RedisRoutingAffinityCoordinator, error) {
	if client == nil {
		return nil, ErrRoutingAffinityCoordinatorConfig
	}
	namespace := strings.TrimSpace(config.Namespace)
	if namespace == "" {
		namespace = redisRoutingAffinityDefaultNamespace
	}
	if !validRedisAIJobDeliveryName(namespace) {
		return nil, fmt.Errorf("%w: invalid namespace", ErrRoutingAffinityCoordinatorConfig)
	}
	return &RedisRoutingAffinityCoordinator{
		client: client, keyPrefix: "asterrouter:{" + namespace + ":routing_affinity}:",
	}, nil
}

func (c *RedisRoutingAffinityCoordinator) Find(ctx context.Context, scopeKey string) (RoutingAffinityBinding, bool, error) {
	key, err := c.key(scopeKey)
	if err != nil {
		return RoutingAffinityBinding{}, false, err
	}
	values, err := c.client.HMGet(ctx, key, "owner", "value").Result()
	if err != nil {
		return RoutingAffinityBinding{}, false, err
	}
	if len(values) != 2 || values[0] == nil && values[1] == nil {
		return RoutingAffinityBinding{}, false, nil
	}
	if values[0] == nil || values[1] == nil {
		return RoutingAffinityBinding{}, false, ErrRoutingAffinityStateInvalid
	}
	payload := redisRoutingAffinityString(values[1])
	binding, err := decodeRedisRoutingAffinity(payload, strings.TrimSpace(scopeKey))
	if err != nil {
		return RoutingAffinityBinding{}, false, err
	}
	if redisRoutingAffinityString(values[0]) != routingAffinityOwner(binding) {
		return RoutingAffinityBinding{}, false, ErrRoutingAffinityStateInvalid
	}
	return binding, true, nil
}

func (c *RedisRoutingAffinityCoordinator) Claim(ctx context.Context, binding RoutingAffinityBinding, ttl time.Duration) (RoutingAffinityBinding, bool, error) {
	if !validRoutingAffinityBinding(binding, ttl) {
		return RoutingAffinityBinding{}, false, ErrRoutingAffinityBindingInvalid
	}
	key, err := c.key(binding.ScopeKey)
	if err != nil {
		return RoutingAffinityBinding{}, false, err
	}
	payload, err := json.Marshal(binding)
	if err != nil {
		return RoutingAffinityBinding{}, false, err
	}
	result, err := redisRoutingAffinityClaimScript.Run(ctx, c.client, []string{key}, routingAffinityOwner(binding), string(payload), max(ttl.Milliseconds(), int64(1))).Result()
	if err != nil {
		return RoutingAffinityBinding{}, false, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return RoutingAffinityBinding{}, false, ErrRoutingAffinityStateInvalid
	}
	code, ok := redisRoutingAffinityInt64(values[0])
	if !ok || code < 0 {
		return RoutingAffinityBinding{}, false, ErrRoutingAffinityStateInvalid
	}
	winner, err := decodeRedisRoutingAffinity(redisRoutingAffinityString(values[1]), binding.ScopeKey)
	if err != nil {
		return RoutingAffinityBinding{}, false, err
	}
	return winner, code == 1, nil
}

func (c *RedisRoutingAffinityCoordinator) Refresh(ctx context.Context, binding RoutingAffinityBinding, ttl time.Duration) (bool, error) {
	if !validRoutingAffinityBinding(binding, ttl) {
		return false, ErrRoutingAffinityBindingInvalid
	}
	key, err := c.key(binding.ScopeKey)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(binding)
	if err != nil {
		return false, err
	}
	result, err := redisRoutingAffinityRefreshScript.Run(ctx, c.client, []string{key}, routingAffinityOwner(binding), string(payload), max(ttl.Milliseconds(), int64(1))).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (c *RedisRoutingAffinityCoordinator) key(scopeKey string) (string, error) {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" || len(scopeKey) > 256 || !validRedisAIJobDeliveryName(scopeKey) {
		return "", ErrRoutingAffinityBindingInvalid
	}
	return c.keyPrefix + scopeKey, nil
}

func routingAffinityOwner(binding RoutingAffinityBinding) string {
	return hashAPIKey(strings.Join([]string{
		binding.Kind, binding.ProviderID, binding.ProviderAccountID, binding.RouteID,
		binding.Model, binding.Protocol, strconv.Itoa(binding.PolicyVersion),
	}, "\x00"))
}

func decodeRedisRoutingAffinity(payload, scopeKey string) (RoutingAffinityBinding, error) {
	var binding RoutingAffinityBinding
	if strings.TrimSpace(payload) == "" || json.Unmarshal([]byte(payload), &binding) != nil || binding.ScopeKey != strings.TrimSpace(scopeKey) || !validRoutingAffinityBinding(binding, time.Millisecond) {
		return RoutingAffinityBinding{}, ErrRoutingAffinityStateInvalid
	}
	return binding, nil
}

func redisRoutingAffinityString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(value)
	}
}

func redisRoutingAffinityInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		parsed, err := strconv.ParseInt(redisRoutingAffinityString(value), 10, 64)
		return parsed, err == nil
	}
}

var redisRoutingAffinityClaimScript = redis.NewScript(`
local current_owner = redis.call('HGET', KEYS[1], 'owner')
if not current_owner then
  redis.call('HSET', KEYS[1], 'owner', ARGV[1], 'value', ARGV[2])
  redis.call('PEXPIRE', KEYS[1], ARGV[3])
  return {1, ARGV[2]}
end
if current_owner == ARGV[1] then
  redis.call('HSET', KEYS[1], 'value', ARGV[2])
  redis.call('PEXPIRE', KEYS[1], ARGV[3])
  return {0, ARGV[2]}
end
local current_value = redis.call('HGET', KEYS[1], 'value')
if not current_value then
  return {-1, ''}
end
return {0, current_value}
`)

var redisRoutingAffinityRefreshScript = redis.NewScript(`
local current_owner = redis.call('HGET', KEYS[1], 'owner')
if not current_owner or current_owner ~= ARGV[1] then
  return 0
end
redis.call('HSET', KEYS[1], 'value', ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1
`)
