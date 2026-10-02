package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
)

const redisQueuePrefix = "nimbus:queue:"
const redisDelayedPrefix = "nimbus:queue:delayed:"
const redisInFlightPrefix = "nimbus:queue:inflight:"
const redisLeasesPrefix = "nimbus:queue:leases:"
const redisReclaimsPrefix = "nimbus:queue:reclaims:"
const redisNotifyPrefix = "nimbus:queue:notify:"

// redisProcessingPrefix is the list older versions moved jobs into with
// BRPOPLPUSH. It is only cleaned up now, never written.
const redisProcessingPrefix = "nimbus:queue:processing:"

// redisMoveLimit caps how many expired leases or due delayed jobs one Pop
// moves, so a large backlog cannot stall Redis inside a single script.
const redisMoveLimit = 100

// redisMaxWait is the longest Pop blocks before re-checking delayed jobs
// and expired leases.
const redisMaxWait = time.Second

// claimScript atomically returns expired leases and due delayed jobs to the
// pending list, then pops the head and leases it to the caller. Running it
// as one script is what makes delivery exactly-once between workers: a job
// can only be moved or claimed by whoever runs the script first.
//
// In-flight leases are members of KEYS[3] named by a per-claim token, with
// the raw payload stored under that token in KEYS[4]. Members written by
// older versions are the raw payload itself and have no entry in KEYS[4].
//
// KEYS: pending, delayed, inflight, leases, reclaims, legacy processing
// ARGV: now, lease deadline, claim token, move limit
var claimScript = redis.NewScript(`
local now = ARGV[1]
local limit = tonumber(ARGV[4])

local function job_id(raw)
  local ok, job = pcall(cjson.decode, raw)
  if ok and type(job) == 'table' and type(job['id']) == 'string' then
    return job['id']
  end
  return nil
end

local reclaimed = 0
local expired = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', now, 'LIMIT', 0, limit)
for _, member in ipairs(expired) do
  redis.call('ZREM', KEYS[3], member)
  local raw = redis.call('HGET', KEYS[4], member)
  if raw then
    redis.call('HDEL', KEYS[4], member)
  else
    raw = member
    redis.call('LREM', KEYS[6], 1, raw)
  end
  redis.call('RPUSH', KEYS[1], raw)
  local id = job_id(raw)
  if id then
    redis.call('HINCRBY', KEYS[5], id, 1)
  end
  reclaimed = reclaimed + 1
end

local due = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now, 'LIMIT', 0, limit)
for _, raw in ipairs(due) do
  redis.call('ZREM', KEYS[2], raw)
  redis.call('RPUSH', KEYS[1], raw)
end

local raw = redis.call('LPOP', KEYS[1])
if not raw then
  return {reclaimed, '', 0}
end
redis.call('ZADD', KEYS[3], ARGV[2], ARGV[3])
redis.call('HSET', KEYS[4], ARGV[3], raw)
local lost = 0
local id = job_id(raw)
if id then
  lost = tonumber(redis.call('HGET', KEYS[5], id) or 0)
end
return {reclaimed, raw, lost}
`)

// completeScript drops a lease, but only while the caller still holds it.
// A worker whose lease expired and was re-claimed by another worker must
// not ack the other worker's delivery.
//
// KEYS: inflight, leases, reclaims
// ARGV: claim token, job id
var completeScript = redis.NewScript(`
if redis.call('ZREM', KEYS[1], ARGV[1]) == 1 then
  redis.call('HDEL', KEYS[2], ARGV[1])
  if ARGV[2] ~= '' then
    redis.call('HDEL', KEYS[3], ARGV[2])
  end
  return 1
end
redis.call('HDEL', KEYS[2], ARGV[1])
return 0
`)

// extendScript pushes a lease deadline out if the lease is still held.
//
// KEYS: inflight
// ARGV: claim token, new deadline
var extendScript = redis.NewScript(`
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  redis.call('ZADD', KEYS[1], 'XX', ARGV[2], ARGV[1])
  return 1
end
return 0
`)

// RedisAdapter stores jobs in Redis: a pending list per queue, a sorted set
// of delayed jobs, and a sorted set of in-flight leases that are returned to
// the queue when a worker stops heartbeating (see LeaseExtender).
type RedisAdapter struct {
	client            *redis.Client
	visibilityTimeout time.Duration
}

// NewRedisAdapter creates a Redis adapter. Pass a configured redis.Client.
func NewRedisAdapter(client *redis.Client) *RedisAdapter {
	return &RedisAdapter{
		client:            client,
		visibilityTimeout: 60 * time.Second,
	}
}

// NewRedisAdapterFromURL creates adapter from REDIS_URL (e.g. redis://localhost:6379).
func NewRedisAdapterFromURL(url string) (*RedisAdapter, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return NewRedisAdapter(redis.NewClient(opt)), nil
}

// Client returns the underlying Redis client.
func (r *RedisAdapter) Client() *redis.Client { return r.client }

// SetVisibilityTimeout sets how long a job stays leased to a worker without
// a heartbeat before it is handed to another worker. Workers run by the
// Manager extend the lease while the job runs, so this bounds how long a
// crashed worker's job waits, not how long a job may run.
func (r *RedisAdapter) SetVisibilityTimeout(v time.Duration) {
	if v > 0 {
		r.visibilityTimeout = v
	}
}

// LeaseDuration implements LeaseExtender.
func (r *RedisAdapter) LeaseDuration() time.Duration { return r.visibilityTimeout }

// Push adds a job to the queue.
func (r *RedisAdapter) Push(ctx context.Context, payload *JobPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if payload.Delay > 0 {
		score := float64(time.Now().Add(payload.Delay).UnixMilli()) / 1000
		return r.client.ZAdd(ctx, redisDelayedPrefix+payload.Queue, redis.Z{Score: score, Member: data}).Err()
	}
	notifyKey := redisNotifyPrefix + payload.Queue
	_, err = r.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.RPush(ctx, redisQueuePrefix+payload.Queue, data)
		// Wake one blocked worker. The token list is only a doorbell; the
		// claim script decides who gets the job, so it is trimmed freely.
		p.RPush(ctx, notifyKey, "1")
		p.LTrim(ctx, notifyKey, -256, -1)
		return nil
	})
	return err
}

// Pop blocks until a job is available or ctx is done.
func (r *RedisAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	keys := []string{
		redisQueuePrefix + queue,
		redisDelayedPrefix + queue,
		redisInFlightPrefix + queue,
		redisLeasesPrefix + queue,
		redisReclaimsPrefix + queue,
		redisProcessingPrefix + queue,
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := newLeaseToken()
		if err != nil {
			return nil, err
		}
		now := time.Now()
		res, err := claimScript.Run(ctx, r.client, keys,
			redisScore(now),
			redisScore(now.Add(r.visibilityTimeout)),
			token,
			redisMoveLimit,
		).Slice()
		if err != nil {
			return nil, err
		}
		reclaimed, raw, lost := parseClaim(res)
		if reclaimed > 0 {
			notifyReclaimed(queue, reclaimed)
		}
		if raw == "" {
			if err := r.wait(ctx, queue); err != nil {
				return nil, err
			}
			continue
		}
		var p JobPayload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			_ = completeScript.Run(ctx, r.client, keys[2:5], token, "").Err()
			continue // skip malformed
		}
		if p.Meta == nil {
			p.Meta = make(map[string]interface{})
		}
		// Deliveries lost to a crashed or stalled worker count as attempts,
		// so a job that kills its worker cannot loop forever.
		p.Attempts += lost
		p.Meta[metaRedisLeaseToken] = token
		return &p, nil
	}
}

// wait blocks until a Push rings the queue's doorbell, ctx is done, or
// redisMaxWait passes (so delayed jobs and expired leases get picked up).
func (r *RedisAdapter) wait(ctx context.Context, queue string) error {
	// BLPOP timeouts are whole seconds for go-redis, so this is also the floor.
	err := r.client.BLPop(ctx, redisMaxWait, redisNotifyPrefix+queue).Err()
	if err == nil || err == redis.Nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// Len returns the number of pending jobs.
func (r *RedisAdapter) Len(ctx context.Context, queue string) (int, error) {
	n, err := r.client.LLen(ctx, redisQueuePrefix+queue).Result()
	return int(n), err
}

// Complete acknowledges and removes a processed in-flight Redis message.
func (r *RedisAdapter) Complete(ctx context.Context, payload *JobPayload) error {
	if payload == nil || payload.Meta == nil {
		return nil
	}
	if token, _ := payload.Meta[metaRedisLeaseToken].(string); token != "" {
		keys := []string{
			redisInFlightPrefix + payload.Queue,
			redisLeasesPrefix + payload.Queue,
			redisReclaimsPrefix + payload.Queue,
		}
		return completeScript.Run(ctx, r.client, keys, token, payload.ID).Err()
	}
	// Delivered by an older version that tracked raw payloads.
	processingKey, _ := payload.Meta["redis_processing_key"].(string)
	inFlightKey, _ := payload.Meta["redis_inflight_key"].(string)
	raw, _ := payload.Meta["redis_raw_payload"].(string)
	if processingKey == "" || inFlightKey == "" || raw == "" {
		return nil
	}
	_, err := r.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.LRem(ctx, processingKey, 1, raw)
		p.ZRem(ctx, inFlightKey, raw)
		return nil
	})
	return err
}

// ExtendLease implements LeaseExtender. It returns ErrLeaseLost when the
// lease already expired and the job was handed to another worker.
func (r *RedisAdapter) ExtendLease(ctx context.Context, payload *JobPayload) error {
	if payload == nil || payload.Meta == nil {
		return nil
	}
	token, _ := payload.Meta[metaRedisLeaseToken].(string)
	if token == "" {
		return nil
	}
	deadline := redisScore(time.Now().Add(r.visibilityTimeout))
	n, err := extendScript.Run(ctx, r.client, []string{redisInFlightPrefix + payload.Queue}, token, deadline).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

const metaRedisLeaseToken = "redis_lease_token"

// redisScore is a Unix timestamp in seconds with millisecond precision.
// Older versions wrote whole seconds, which compare correctly against it.
func redisScore(t time.Time) string {
	return fmt.Sprintf("%.3f", float64(t.UnixMilli())/1000)
}

func newLeaseToken() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func parseClaim(res []interface{}) (reclaimed int, raw string, lost int) {
	if len(res) != 3 {
		return 0, "", 0
	}
	if n, ok := res[0].(int64); ok {
		reclaimed = int(n)
	}
	raw, _ = res[1].(string)
	if n, ok := res[2].(int64); ok {
		lost = int(n)
	}
	return reclaimed, raw, lost
}

var _ Adapter = (*RedisAdapter)(nil)
var _ CompletableAdapter = (*RedisAdapter)(nil)
var _ LeaseExtender = (*RedisAdapter)(nil)
