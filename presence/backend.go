package presence

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
)

// routed is an event on its way to the connections of one channel.
type routed struct {
	Channel    string          `json:"channel"`
	Data       json.RawMessage `json:"data"`
	ExceptUser string          `json:"except_user,omitempty"`
	ToUser     string          `json:"to_user,omitempty"`
}

// backend tracks who is in which channel and carries events between hub
// instances. Membership is counted per connection, so a user with two tabs
// open joins once and leaves when the last tab closes.
type backend interface {
	// join records a connection and reports whether it is the user's first
	// in the channel.
	join(ctx context.Context, channel string, c *Client) (first bool, err error)
	// leave drops a connection and reports whether it was the user's last.
	leave(ctx context.Context, channel string, c *Client) (last bool, err error)
	users(ctx context.Context, channel string) ([]User, error)
	userCount(ctx context.Context, channel string) (int, error)
	channels(ctx context.Context) ([]string, error)
	publish(ctx context.Context, r routed) error
	close() error
}

// ── local ───────────────────────────────────────────────────────────

type localBackend struct {
	hub   *Hub
	mu    sync.Mutex
	conns map[string]map[string]int  // channel -> user ID -> connections
	info  map[string]map[string]User // channel -> user ID -> latest user info
}

func newLocalBackend(h *Hub) *localBackend {
	return &localBackend{hub: h, conns: map[string]map[string]int{}, info: map[string]map[string]User{}}
}

func (b *localBackend) join(_ context.Context, channel string, c *Client) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conns[channel] == nil {
		b.conns[channel] = map[string]int{}
		b.info[channel] = map[string]User{}
	}
	b.conns[channel][c.user.ID]++
	b.info[channel][c.user.ID] = *c.user
	return b.conns[channel][c.user.ID] == 1, nil
}

func (b *localBackend) leave(_ context.Context, channel string, c *Client) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	counts := b.conns[channel]
	if counts == nil || counts[c.user.ID] == 0 {
		return false, nil
	}
	counts[c.user.ID]--
	if counts[c.user.ID] > 0 {
		return false, nil
	}
	delete(counts, c.user.ID)
	delete(b.info[channel], c.user.ID)
	if len(counts) == 0 {
		delete(b.conns, channel)
		delete(b.info, channel)
	}
	return true, nil
}

func (b *localBackend) users(_ context.Context, channel string) ([]User, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]User, 0, len(b.info[channel]))
	for _, u := range b.info[channel] {
		out = append(out, u)
	}
	sortUsers(out)
	return out, nil
}

func (b *localBackend) userCount(_ context.Context, channel string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.conns[channel]), nil
}

func (b *localBackend) channels(context.Context) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.conns))
	for name := range b.conns {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (b *localBackend) publish(_ context.Context, r routed) error {
	b.hub.deliver(r)
	return nil
}

func (b *localBackend) close() error { return nil }

// ── redis ───────────────────────────────────────────────────────────

// Per channel, Redis holds:
//   <prefix>c:<channel>:conns  zset  connection key -> lease expiry (ms)
//   <prefix>c:<channel>:data   hash  connection key -> user JSON
//   <prefix>c:<channel>:users  hash  user ID -> connection count
// plus <prefix>channels, the set of channels with anyone in them.
// Each hub refreshes its connections' leases every ping interval, so the
// connections of a crashed instance expire and are pruned (with leave
// events) by whichever instance looks at the channel next.

var presenceJoinScript = redis.NewScript(`
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[1])
redis.call('HSET', KEYS[2], ARGV[1], ARGV[4])
redis.call('SADD', KEYS[4], ARGV[5])
return redis.call('HINCRBY', KEYS[3], ARGV[2], 1)
`)

var presenceLeaveScript = redis.NewScript(`
if redis.call('ZREM', KEYS[1], ARGV[1]) == 0 then
  return -1
end
redis.call('HDEL', KEYS[2], ARGV[1])
local n = redis.call('HINCRBY', KEYS[3], ARGV[2], -1)
if n <= 0 then
  redis.call('HDEL', KEYS[3], ARGV[2])
  n = 0
end
if redis.call('HLEN', KEYS[3]) == 0 then
  redis.call('SREM', KEYS[4], ARGV[3])
end
return n
`)

// presencePruneScript drops expired connections and returns the JSON of
// users whose last connection that was.
var presencePruneScript = redis.NewScript(`
local gone = {}
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, 500)
for _, conn in ipairs(expired) do
  redis.call('ZREM', KEYS[1], conn)
  local raw = redis.call('HGET', KEYS[2], conn)
  redis.call('HDEL', KEYS[2], conn)
  if raw then
    local ok, user = pcall(cjson.decode, raw)
    if ok and type(user) == 'table' and user['id'] then
      local id = tostring(user['id'])
      local n = redis.call('HINCRBY', KEYS[3], id, -1)
      if n <= 0 then
        redis.call('HDEL', KEYS[3], id)
        table.insert(gone, raw)
      end
    end
  end
end
if redis.call('HLEN', KEYS[3]) == 0 then
  redis.call('SREM', KEYS[4], ARGV[2])
end
return gone
`)

type redisBackend struct {
	hub    *Hub
	client *redis.Client
	prefix string
	topic  string
	lease  time.Duration
	pubsub *redis.PubSub
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newRedisBackend(h *Hub, client *redis.Client, prefix string) *redisBackend {
	if prefix == "" {
		prefix = "nimbus:presence:"
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &redisBackend{
		hub:    h,
		client: client,
		prefix: prefix,
		topic:  prefix + "events",
		lease:  3 * h.config.PingInterval,
		cancel: cancel,
	}
	b.pubsub = client.Subscribe(ctx, b.topic)
	// Wait for the subscription so events published right after NewHub
	// returns are not missed.
	if _, err := b.pubsub.Receive(ctx); err != nil {
		log.Printf("[presence] redis subscribe: %v", err)
	}
	b.wg.Add(2)
	go b.listen(ctx)
	go b.refreshLoop(ctx)
	return b
}

func (b *redisBackend) keys(channel string) []string {
	base := b.prefix + "c:" + channel
	return []string{base + ":conns", base + ":data", base + ":users", b.prefix + "channels"}
}

func (b *redisBackend) connKey(c *Client) string {
	return b.hub.instanceID + ":" + c.id
}

func (b *redisBackend) expiry() int64 {
	return time.Now().Add(b.lease).UnixMilli()
}

func (b *redisBackend) join(ctx context.Context, channel string, c *Client) (bool, error) {
	b.prune(ctx, channel)
	userJSON, err := json.Marshal(c.user)
	if err != nil {
		return false, err
	}
	n, err := presenceJoinScript.Run(ctx, b.client, b.keys(channel),
		b.connKey(c), c.user.ID, b.expiry(), string(userJSON), channel).Int()
	return n == 1, err
}

func (b *redisBackend) leave(ctx context.Context, channel string, c *Client) (bool, error) {
	n, err := presenceLeaveScript.Run(ctx, b.client, b.keys(channel), b.connKey(c), c.user.ID, channel).Int()
	return n == 0, err
}

// prune expires dead connections in channel and announces users who left
// with them.
func (b *redisBackend) prune(ctx context.Context, channel string) {
	gone, err := presencePruneScript.Run(ctx, b.client, b.keys(channel), time.Now().UnixMilli(), channel).StringSlice()
	if err != nil && err != redis.Nil {
		log.Printf("[presence] prune %q: %v", channel, err)
		return
	}
	for _, raw := range gone {
		var u User
		if json.Unmarshal([]byte(raw), &u) != nil {
			continue
		}
		b.hub.publish(ctx, channel, Event{Type: "presence:leave", Channel: channel, User: &u}, "", "")
	}
}

func (b *redisBackend) users(ctx context.Context, channel string) ([]User, error) {
	b.prune(ctx, channel)
	vals, err := b.client.HVals(ctx, b.keys(channel)[1]).Result()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]User, 0, len(vals))
	for _, raw := range vals {
		var u User
		if json.Unmarshal([]byte(raw), &u) != nil || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		out = append(out, u)
	}
	sortUsers(out)
	return out, nil
}

func (b *redisBackend) userCount(ctx context.Context, channel string) (int, error) {
	b.prune(ctx, channel)
	n, err := b.client.HLen(ctx, b.keys(channel)[2]).Result()
	return int(n), err
}

func (b *redisBackend) channels(ctx context.Context) ([]string, error) {
	names, err := b.client.SMembers(ctx, b.prefix+"channels").Result()
	if err != nil {
		return nil, err
	}
	out := names[:0]
	for _, name := range names {
		if n, err := b.userCount(ctx, name); err == nil && n > 0 {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (b *redisBackend) publish(ctx context.Context, r routed) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, b.topic, data).Err()
}

func (b *redisBackend) listen(ctx context.Context) {
	defer b.wg.Done()
	msgs := b.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			var r routed
			if json.Unmarshal([]byte(msg.Payload), &r) == nil && r.Channel != "" {
				b.hub.deliver(r)
			}
		}
	}
}

// refreshLoop renews this instance's connection leases and prunes the
// channels it serves.
func (b *redisBackend) refreshLoop(ctx context.Context) {
	defer b.wg.Done()
	t := time.NewTicker(b.hub.config.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			exp := float64(b.expiry())
			for name, clients := range b.hub.localClients() {
				conns := b.keys(name)[0]
				members := make([]redis.Z, 0, len(clients))
				for _, c := range clients {
					members = append(members, redis.Z{Score: exp, Member: b.connKey(c)})
				}
				if len(members) > 0 {
					if err := b.client.ZAddXX(ctx, conns, members...).Err(); err != nil {
						log.Printf("[presence] refresh %q: %v", name, err)
					}
				}
				b.prune(ctx, name)
			}
		}
	}
}

func (b *redisBackend) close() error {
	b.cancel()
	_ = b.pubsub.Close()
	b.wg.Wait()
	return nil
}

func sortUsers(us []User) {
	sort.Slice(us, func(i, j int) bool { return us[i].ID < us[j].ID })
}
