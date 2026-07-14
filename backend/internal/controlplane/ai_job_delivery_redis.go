package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRedisAIJobDeliveryConfig         = errors.New("invalid redis ai job delivery queue configuration")
	ErrAIJobDeliveryEnvelopeTooLarge    = errors.New("ai job delivery envelope exceeds the configured payload limit")
	ErrAIJobDeliveryInfrastructureState = errors.New("ai job delivery queue infrastructure state is invalid")
)

const (
	redisAIJobDeliveryDefaultNamespace      = "asterrouter"
	redisAIJobDeliveryDefaultConsumerGroup  = "asterrouter-workers"
	redisAIJobDeliveryDefaultLease          = 30 * time.Second
	redisAIJobDeliveryDefaultDedupeTTL      = 7 * 24 * time.Hour
	redisAIJobDeliveryDefaultMaxPayload     = 16 * 1024
	redisAIJobDeliveryDefaultPromotionBatch = int64(100)
	redisAIJobDeliveryDefaultDeadLetterMax  = int64(10_000)
	redisAIJobDeliveryMaxReasonBytes        = 2048
)

type RedisAIJobDeliveryQueueConfig struct {
	Namespace        string
	ConsumerGroup    string
	DeliveryLease    time.Duration
	DedupeTTL        time.Duration
	MaxPayloadBytes  int
	PromotionBatch   int64
	DeadLetterMaxLen int64
}

type RedisAIJobDeliveryQueue struct {
	client         redis.UniversalClient
	consumerGroup  string
	deliveryLease  time.Duration
	dedupeTTL      time.Duration
	maxPayload     int
	promotionBatch int64
	deadLetterMax  int64
	streamKey      string
	delayedKey     string
	leaseTokenKey  string
	leaseUntilKey  string
	attemptKey     string
	deadLetterKey  string
	dedupePrefix   string
}

var _ AIJobDeliveryQueue = (*RedisAIJobDeliveryQueue)(nil)

func NewRedisAIJobDeliveryQueue(client redis.UniversalClient, config RedisAIJobDeliveryQueueConfig) (*RedisAIJobDeliveryQueue, error) {
	if client == nil {
		return nil, ErrRedisAIJobDeliveryConfig
	}
	namespace := strings.TrimSpace(config.Namespace)
	if namespace == "" {
		namespace = redisAIJobDeliveryDefaultNamespace
	}
	if !validRedisAIJobDeliveryName(namespace) {
		return nil, fmt.Errorf("%w: invalid namespace", ErrRedisAIJobDeliveryConfig)
	}
	consumerGroup := strings.TrimSpace(config.ConsumerGroup)
	if consumerGroup == "" {
		consumerGroup = redisAIJobDeliveryDefaultConsumerGroup
	}
	if !validRedisAIJobDeliveryName(consumerGroup) {
		return nil, fmt.Errorf("%w: invalid consumer group", ErrRedisAIJobDeliveryConfig)
	}
	deliveryLease := config.DeliveryLease
	if deliveryLease == 0 {
		deliveryLease = redisAIJobDeliveryDefaultLease
	}
	dedupeTTL := config.DedupeTTL
	if dedupeTTL == 0 {
		dedupeTTL = redisAIJobDeliveryDefaultDedupeTTL
	}
	maxPayload := config.MaxPayloadBytes
	if maxPayload == 0 {
		maxPayload = redisAIJobDeliveryDefaultMaxPayload
	}
	promotionBatch := config.PromotionBatch
	if promotionBatch == 0 {
		promotionBatch = redisAIJobDeliveryDefaultPromotionBatch
	}
	deadLetterMax := config.DeadLetterMaxLen
	if deadLetterMax == 0 {
		deadLetterMax = redisAIJobDeliveryDefaultDeadLetterMax
	}
	if deliveryLease < time.Millisecond || dedupeTTL < time.Millisecond || maxPayload <= 0 || promotionBatch <= 0 || deadLetterMax <= 0 {
		return nil, ErrRedisAIJobDeliveryConfig
	}
	prefix := "asterrouter:{" + namespace + ":ai_job_delivery}"
	return &RedisAIJobDeliveryQueue{
		client: client, consumerGroup: consumerGroup, deliveryLease: deliveryLease, dedupeTTL: dedupeTTL,
		maxPayload: maxPayload, promotionBatch: promotionBatch, deadLetterMax: deadLetterMax,
		streamKey: prefix + ":stream", delayedKey: prefix + ":delayed",
		leaseTokenKey: prefix + ":lease_tokens", leaseUntilKey: prefix + ":lease_until",
		attemptKey: prefix + ":attempts", deadLetterKey: prefix + ":dead_letters", dedupePrefix: prefix + ":dedupe:",
	}, nil
}

func (q *RedisAIJobDeliveryQueue) Publish(ctx context.Context, envelope AIJobDeliveryEnvelope, dedupeKey string, availableAt time.Time) error {
	payload, err := q.marshalEnvelope(envelope)
	if err != nil {
		return err
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = envelope.DedupeKey()
	}
	if dedupeKey == "" {
		return ErrAIJobDeliveryEnvelopeInvalid
	}
	now := time.Now().UTC()
	if availableAt.IsZero() {
		availableAt = now
	}
	delay := availableAt.Sub(now)
	if delay < 0 {
		delay = 0
	}
	delayed, err := json.Marshal(redisDelayedAIJobDelivery{Envelope: string(payload)})
	if err != nil {
		return err
	}
	result, err := redisAIJobDeliveryPublishScript.Run(ctx, q.client,
		[]string{q.streamKey, q.delayedKey, q.dedupeKey(dedupeKey), q.dedupeKey(envelope.DedupeKey())},
		string(payload), q.dedupeTTL.Milliseconds(), delay.Milliseconds(), string(delayed),
	).Int64()
	if err != nil {
		return err
	}
	switch result {
	case 1, 0:
		return nil
	case -1:
		return ErrAIJobDeliveryDedupeConflict
	default:
		return ErrAIJobDeliveryInfrastructureState
	}
}

func (q *RedisAIJobDeliveryQueue) Receive(ctx context.Context, consumer string, maxItems int, wait time.Duration) ([]AIJobDelivery, error) {
	consumer = strings.TrimSpace(consumer)
	if consumer == "" || maxItems <= 0 {
		return []AIJobDelivery{}, nil
	}
	if wait < 0 {
		wait = 0
	}
	if err := q.ensureConsumerGroup(ctx); err != nil {
		return nil, err
	}
	deadline := time.Time{}
	if wait > 0 {
		deadline = time.Now().Add(wait)
	}
	for {
		if err := q.promoteDue(ctx); err != nil {
			return nil, err
		}
		reclaimed, err := q.reclaimExpired(ctx, consumer, maxItems)
		if err != nil {
			return nil, err
		}
		if len(reclaimed) > 0 {
			return reclaimed, nil
		}
		block, done, err := q.receiveBlock(ctx, deadline)
		if err != nil {
			return nil, err
		}
		if done {
			return []AIJobDelivery{}, nil
		}
		streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: q.consumerGroup, Consumer: consumer, Streams: []string{q.streamKey, ">"}, Count: int64(maxItems), Block: block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			if deadline.IsZero() {
				return []AIJobDelivery{}, nil
			}
			continue
		}
		if err != nil {
			if redisAIJobDeliveryNoGroup(err) {
				if groupErr := q.ensureConsumerGroup(ctx); groupErr == nil {
					continue
				}
			}
			return nil, err
		}
		messages := flattenRedisAIJobMessages(streams)
		if len(messages) == 0 {
			if deadline.IsZero() {
				return []AIJobDelivery{}, nil
			}
			continue
		}
		deliveries, err := q.establishNewLeases(ctx, consumer, messages)
		if err != nil {
			return nil, err
		}
		if len(deliveries) > 0 {
			return deliveries, nil
		}
	}
}

func (q *RedisAIJobDeliveryQueue) Extend(ctx context.Context, delivery AIJobDelivery, leaseUntil time.Time) error {
	leaseDuration := time.Until(leaseUntil)
	if leaseDuration <= 0 {
		return ErrAIJobDeliveryLeaseExpired
	}
	result, err := redisAIJobDeliveryExtendScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey},
		q.consumerGroup, delivery.Consumer, delivery.ID, delivery.LeaseToken, max(leaseDuration.Milliseconds(), int64(1)),
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) Ack(ctx context.Context, delivery AIJobDelivery) error {
	result, err := redisAIJobDeliveryAckScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, delivery.ID, delivery.LeaseToken,
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) Nack(ctx context.Context, delivery AIJobDelivery, retryAt time.Time, reason string) error {
	payload, err := q.marshalEnvelope(delivery.Envelope)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if retryAt.IsZero() || retryAt.Before(now) {
		retryAt = now
	}
	retryDelay := retryAt.Sub(now)
	reason = trimRedisAIJobDeliveryReason(reason)
	delayed, err := json.Marshal(redisDelayedAIJobDelivery{Envelope: string(payload), AttemptBase: delivery.Attempt, LastError: reason})
	if err != nil {
		return err
	}
	result, err := redisAIJobDeliveryNackScript.Run(ctx, q.client,
		[]string{q.streamKey, q.delayedKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, delivery.ID, delivery.LeaseToken, retryDelay.Milliseconds(),
		string(payload), delivery.Attempt, reason, string(delayed),
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) DeadLetter(ctx context.Context, delivery AIJobDelivery, reason string) error {
	payload, err := q.marshalEnvelope(delivery.Envelope)
	if err != nil {
		return err
	}
	result, err := redisAIJobDeliveryDeadLetterScript.Run(ctx, q.client,
		[]string{q.streamKey, q.deadLetterKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, delivery.ID, delivery.LeaseToken, string(payload), delivery.Attempt, trimRedisAIJobDeliveryReason(reason), q.deadLetterMax,
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) ensureConsumerGroup(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.streamKey, q.consumerGroup, "0").Err()
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (q *RedisAIJobDeliveryQueue) promoteDue(ctx context.Context) error {
	_, err := redisAIJobDeliveryPromoteScript.Run(ctx, q.client,
		[]string{q.streamKey, q.delayedKey, q.deadLetterKey}, q.promotionBatch, q.deadLetterMax,
	).Result()
	return err
}

func (q *RedisAIJobDeliveryQueue) reclaimExpired(ctx context.Context, consumer string, maxItems int) ([]AIJobDelivery, error) {
	scanCount := maxItems * 10
	if scanCount < 100 {
		scanCount = 100
	}
	values, err := redisAIJobDeliveryReclaimScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, consumer, maxItems, q.deliveryLease.Milliseconds(), "delivery_lease_"+randomID(16),
		(q.deliveryLease * 2).Milliseconds(), scanCount,
	).StringSlice()
	if err != nil {
		return nil, err
	}
	return q.decodeClaimedDeliveries(ctx, consumer, values)
}

func (q *RedisAIJobDeliveryQueue) establishNewLeases(ctx context.Context, consumer string, messages []redis.XMessage) ([]AIJobDelivery, error) {
	localLeaseUntil := time.Now().UTC().Add(q.deliveryLease)
	args := make([]any, 0, 4+2*len(messages))
	args = append(args, q.consumerGroup, consumer, q.deliveryLease.Milliseconds(), "delivery_lease_"+randomID(16))
	byID := make(map[string]redis.XMessage, len(messages))
	for _, message := range messages {
		byID[message.ID] = message
		args = append(args, message.ID, redisAIJobDeliveryAttemptBase(message.Values))
	}
	values, err := redisAIJobDeliveryEstablishScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey}, args...,
	).StringSlice()
	if err != nil {
		return nil, err
	}
	deliveries := make([]AIJobDelivery, 0, len(values))
	for _, raw := range values {
		var lease redisAIJobDeliveryLeaseResultValue
		if err := json.Unmarshal([]byte(raw), &lease); err != nil {
			return nil, err
		}
		message, found := byID[lease.ID]
		if !found {
			return nil, ErrAIJobDeliveryInfrastructureState
		}
		delivery, err := q.deliveryFromValues(lease.ID, consumer, lease.Token, lease.Attempt, localLeaseUntil, message.Values)
		if err != nil {
			if rejectErr := q.deadLetterMalformed(ctx, lease.ID, lease.Token, lease.Attempt, redisAIJobDeliveryString(message.Values["envelope"]), err); rejectErr != nil {
				return nil, errors.Join(err, rejectErr)
			}
			continue
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (q *RedisAIJobDeliveryQueue) decodeClaimedDeliveries(ctx context.Context, consumer string, values []string) ([]AIJobDelivery, error) {
	localLeaseUntil := time.Now().UTC().Add(q.deliveryLease)
	deliveries := make([]AIJobDelivery, 0, len(values))
	for _, raw := range values {
		var claimed redisAIJobDeliveryClaimedValue
		if err := json.Unmarshal([]byte(raw), &claimed); err != nil {
			return nil, err
		}
		values := make(map[string]any, len(claimed.Fields))
		for key, value := range claimed.Fields {
			values[key] = value
		}
		delivery, err := q.deliveryFromValues(claimed.ID, consumer, claimed.Token, claimed.Attempt, localLeaseUntil, values)
		if err != nil {
			if rejectErr := q.deadLetterMalformed(ctx, claimed.ID, claimed.Token, claimed.Attempt, claimed.Fields["envelope"], err); rejectErr != nil {
				return nil, errors.Join(err, rejectErr)
			}
			continue
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (q *RedisAIJobDeliveryQueue) deliveryFromValues(id, consumer, leaseToken string, attempt int, leaseUntil time.Time, values map[string]any) (AIJobDelivery, error) {
	rawEnvelope := redisAIJobDeliveryString(values["envelope"])
	if len(rawEnvelope) > q.maxPayload {
		return AIJobDelivery{}, ErrAIJobDeliveryEnvelopeTooLarge
	}
	var envelope AIJobDeliveryEnvelope
	if err := json.Unmarshal([]byte(rawEnvelope), &envelope); err != nil {
		return AIJobDelivery{}, err
	}
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		return AIJobDelivery{}, err
	}
	return AIJobDelivery{ID: id, Envelope: envelope, Consumer: consumer, Attempt: attempt, LeaseUntil: leaseUntil, LeaseToken: leaseToken}, nil
}

func (q *RedisAIJobDeliveryQueue) deadLetterMalformed(ctx context.Context, id, leaseToken string, attempt int, payload string, cause error) error {
	if len(payload) > q.maxPayload {
		payload = payload[:q.maxPayload]
	}
	result, err := redisAIJobDeliveryDeadLetterScript.Run(ctx, q.client,
		[]string{q.streamKey, q.deadLetterKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, id, leaseToken, payload, attempt, trimRedisAIJobDeliveryReason(cause.Error()), q.deadLetterMax,
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) receiveBlock(ctx context.Context, deadline time.Time) (time.Duration, bool, error) {
	if deadline.IsZero() {
		return -1, false, nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, true, nil
	}
	pipe := q.client.Pipeline()
	nextDelayed := pipe.ZRangeWithScores(ctx, q.delayedKey, 0, 0)
	nextLease := pipe.ZRangeWithScores(ctx, q.leaseUntilKey, 0, 0)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return 0, false, err
	}
	nextWake := time.Time{}
	for _, values := range [][]redis.Z{nextDelayed.Val(), nextLease.Val()} {
		if len(values) == 0 {
			continue
		}
		candidate := time.UnixMilli(int64(values[0].Score))
		if nextWake.IsZero() || candidate.Before(nextWake) {
			nextWake = candidate
		}
	}
	if !nextWake.IsZero() {
		untilNext := time.Until(nextWake)
		if untilNext < remaining {
			remaining = untilNext
		}
	}
	if q.deliveryLease < remaining {
		remaining = q.deliveryLease
	}
	if remaining < time.Millisecond {
		remaining = time.Millisecond
	}
	return remaining, false, nil
}

func (q *RedisAIJobDeliveryQueue) marshalEnvelope(envelope AIJobDeliveryEnvelope) ([]byte, error) {
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > q.maxPayload {
		return nil, ErrAIJobDeliveryEnvelopeTooLarge
	}
	return payload, nil
}

func (q *RedisAIJobDeliveryQueue) dedupeKey(value string) string {
	return q.dedupePrefix + prefix(hashAPIKey(value), 40)
}

func redisAIJobDeliveryLeaseResult(result int64, err error) error {
	if err != nil {
		return err
	}
	switch result {
	case 1:
		return nil
	case 0:
		return ErrAIJobDeliveryLeaseConflict
	case -1:
		return ErrAIJobDeliveryLeaseExpired
	case -2:
		return ErrAIJobDeliveryNotFound
	default:
		return ErrAIJobDeliveryInfrastructureState
	}
}

func flattenRedisAIJobMessages(streams []redis.XStream) []redis.XMessage {
	var messages []redis.XMessage
	for _, stream := range streams {
		messages = append(messages, stream.Messages...)
	}
	return messages
}

func redisAIJobDeliveryAttemptBase(values map[string]any) int {
	value, err := strconv.Atoi(redisAIJobDeliveryString(values["attempt_base"]))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func redisAIJobDeliveryString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func redisAIJobDeliveryNoGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NOGROUP")
}

func validRedisAIJobDeliveryName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func trimRedisAIJobDeliveryReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= redisAIJobDeliveryMaxReasonBytes {
		return value
	}
	return value[:redisAIJobDeliveryMaxReasonBytes]
}

type redisDelayedAIJobDelivery struct {
	Envelope    string `json:"envelope"`
	AttemptBase int    `json:"attempt_base"`
	LastError   string `json:"last_error,omitempty"`
}

type redisAIJobDeliveryLeaseResultValue struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	Attempt    int    `json:"attempt"`
	LeaseUntil int64  `json:"lease_until"`
}

type redisAIJobDeliveryClaimedValue struct {
	ID         string            `json:"id"`
	Token      string            `json:"token"`
	Attempt    int               `json:"attempt"`
	LeaseUntil int64             `json:"lease_until"`
	Fields     map[string]string `json:"fields"`
}

var redisAIJobDeliveryPublishScript = redis.NewScript(`
local callerExisting = redis.call('GET', KEYS[3])
local canonicalExisting = redis.call('GET', KEYS[4])
if callerExisting and callerExisting ~= ARGV[1] then return -1 end
if canonicalExisting and canonicalExisting ~= ARGV[1] then return -1 end
if callerExisting or canonicalExisting then
  if not callerExisting then redis.call('SET', KEYS[3], ARGV[1], 'PX', ARGV[2]) end
  if not canonicalExisting then redis.call('SET', KEYS[4], ARGV[1], 'PX', ARGV[2]) end
  return 0
end
redis.call('SET', KEYS[3], ARGV[1], 'PX', ARGV[2])
if KEYS[4] ~= KEYS[3] then redis.call('SET', KEYS[4], ARGV[1], 'PX', ARGV[2]) end
local published
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local availableAt = now + tonumber(ARGV[3])
if tonumber(ARGV[3]) <= 0 then
  published = redis.pcall('XADD', KEYS[1], '*', 'envelope', ARGV[1], 'attempt_base', '0')
else
  published = redis.pcall('ZADD', KEYS[2], availableAt, ARGV[4])
end
if type(published) == 'table' and published.err then
  redis.call('DEL', KEYS[3])
  if KEYS[4] ~= KEYS[3] then redis.call('DEL', KEYS[4]) end
  return published
end
return 1
`)

var redisAIJobDeliveryPromoteScript = redis.NewScript(`
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local members = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now, 'LIMIT', 0, ARGV[1])
for _, member in ipairs(members) do
  local decoded, data = pcall(cjson.decode, member)
  if decoded and type(data) == 'table' and type(data.envelope) == 'string' then
    redis.call('XADD', KEYS[1], '*', 'envelope', data.envelope, 'attempt_base', tostring(data.attempt_base or 0), 'last_error', data.last_error or '')
  else
    redis.call('XADD', KEYS[3], 'MAXLEN', '~', ARGV[2], '*', 'envelope', member, 'attempt', '0', 'reason', 'invalid_delayed_envelope', 'failed_at', now)
  end
  redis.call('ZREM', KEYS[2], member)
end
return #members
`)

var redisAIJobDeliveryReclaimScript = redis.NewScript(`
local output = {}
local claimedCount = 0
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local leaseUntil = now + tonumber(ARGV[4])
local function cleanup(id)
  redis.call('HDEL', KEYS[2], id)
  redis.call('ZREM', KEYS[3], id)
  redis.call('HDEL', KEYS[4], id)
end
local function claim(id, priorDeliveries)
  local claimed = redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, id)
  if #claimed == 0 then
    cleanup(id)
    return
  end
  local message = claimed[1]
  local fields = {}
  local attemptBase = 0
  for index = 1, #message[2], 2 do
    local key = message[2][index]
    local value = message[2][index + 1]
    fields[key] = value
    if key == 'attempt_base' then attemptBase = tonumber(value) or 0 end
  end
  local token = ARGV[5] .. ':' .. id
  redis.call('HSET', KEYS[2], id, token)
  redis.call('ZADD', KEYS[3], leaseUntil, id)
  if priorDeliveries and not redis.call('HGET', KEYS[4], id) then
    redis.call('HSET', KEYS[4], id, priorDeliveries)
  else
    redis.call('HSETNX', KEYS[4], id, attemptBase)
  end
  local attempt = redis.call('HINCRBY', KEYS[4], id, 1)
  table.insert(output, cjson.encode({id=id, token=token, attempt=attempt, lease_until=leaseUntil, fields=fields}))
  claimedCount = claimedCount + 1
end
local due = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', now, 'LIMIT', 0, ARGV[3])
for _, id in ipairs(due) do
  claim(id, nil)
end
if claimedCount < tonumber(ARGV[3]) then
  local pending = redis.call('XPENDING', KEYS[1], ARGV[1], 'IDLE', ARGV[6], '-', '+', ARGV[7])
  for _, entry in ipairs(pending) do
    if claimedCount >= tonumber(ARGV[3]) then break end
    local id = entry[1]
    if not redis.call('ZSCORE', KEYS[3], id) then claim(id, tonumber(entry[4]) or 1) end
  end
end
return output
`)

var redisAIJobDeliveryEstablishScript = redis.NewScript(`
local output = {}
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local leaseUntil = now + tonumber(ARGV[3])
for index = 5, #ARGV, 2 do
  local id = ARGV[index]
  local claimed = redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, id, 'IDLE', 0, 'JUSTID')
  if #claimed > 0 then
    local token = ARGV[4] .. ':' .. id
    redis.call('HSET', KEYS[2], id, token)
    redis.call('ZADD', KEYS[3], leaseUntil, id)
    redis.call('HSETNX', KEYS[4], id, tonumber(ARGV[index + 1]) or 0)
    local attempt = redis.call('HINCRBY', KEYS[4], id, 1)
    table.insert(output, cjson.encode({id=id, token=token, attempt=attempt, lease_until=leaseUntil}))
  end
end
return output
`)

var redisAIJobDeliveryExtendScript = redis.NewScript(`
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local token = redis.call('HGET', KEYS[2], ARGV[3])
if not token then return -2 end
if token ~= ARGV[4] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[3], ARGV[3]) or '0')
if leaseUntil <= now then return -1 end
local claimed = redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, ARGV[3], 'IDLE', 0, 'JUSTID')
if #claimed == 0 then
  redis.call('HDEL', KEYS[2], ARGV[3])
  redis.call('ZREM', KEYS[3], ARGV[3])
  return -2
end
local requestedUntil = now + tonumber(ARGV[5])
if requestedUntil > leaseUntil then redis.call('ZADD', KEYS[3], requestedUntil, ARGV[3]) end
return 1
`)

var redisAIJobDeliveryAckScript = redis.NewScript(`
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local token = redis.call('HGET', KEYS[2], ARGV[2])
if not token then return -2 end
if token ~= ARGV[3] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[3], ARGV[2]) or '0')
if leaseUntil <= now then return -1 end
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
redis.call('HDEL', KEYS[2], ARGV[2])
redis.call('ZREM', KEYS[3], ARGV[2])
redis.call('HDEL', KEYS[4], ARGV[2])
if acknowledged == 0 then return -2 end
redis.call('XDEL', KEYS[1], ARGV[2])
return 1
`)

var redisAIJobDeliveryNackScript = redis.NewScript(`
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local retryAt = now + tonumber(ARGV[4])
local token = redis.call('HGET', KEYS[3], ARGV[2])
if not token then return -2 end
if token ~= ARGV[3] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[4], ARGV[2]) or '0')
if leaseUntil <= now then return -1 end
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
if acknowledged == 0 then return -2 end
redis.call('XDEL', KEYS[1], ARGV[2])
redis.call('HDEL', KEYS[3], ARGV[2])
redis.call('ZREM', KEYS[4], ARGV[2])
redis.call('HDEL', KEYS[5], ARGV[2])
if tonumber(ARGV[4]) <= 0 then
  redis.call('XADD', KEYS[1], '*', 'envelope', ARGV[5], 'attempt_base', ARGV[6], 'last_error', ARGV[7])
else
  redis.call('ZADD', KEYS[2], retryAt, ARGV[8])
end
return 1
`)

var redisAIJobDeliveryDeadLetterScript = redis.NewScript(`
local serverTime = redis.call('TIME')
local now = tonumber(serverTime[1]) * 1000 + math.floor(tonumber(serverTime[2]) / 1000)
local token = redis.call('HGET', KEYS[3], ARGV[2])
if not token then return -2 end
if token ~= ARGV[3] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[4], ARGV[2]) or '0')
if leaseUntil <= now then return -1 end
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
if acknowledged == 0 then return -2 end
redis.call('XDEL', KEYS[1], ARGV[2])
redis.call('HDEL', KEYS[3], ARGV[2])
redis.call('ZREM', KEYS[4], ARGV[2])
redis.call('HDEL', KEYS[5], ARGV[2])
redis.call('XADD', KEYS[2], 'MAXLEN', '~', ARGV[7], '*', 'envelope', ARGV[4], 'attempt', ARGV[5], 'reason', ARGV[6], 'failed_at', now)
return 1
`)
