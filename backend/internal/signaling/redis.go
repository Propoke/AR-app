package signaling

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisFanout distributes signaling frames across backend instances via Redis
// pub/sub. Every instance subscribes to one channel; a published frame is
// delivered to the local peer of the target role on whichever instance holds it.
//
// NOTE: exercised in production multi-replica deployments; it needs a two-instance
// integration test before relying on horizontal scaling (the single-instance path
// is covered by LocalFanout tests).
type RedisFanout struct {
	rdb     *redis.Client
	channel string
	log     *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	pubsub  *redis.PubSub
}

type fanoutMessage struct {
	Room       string `json:"room"`
	TargetRole Role   `json:"role"`
	Frame      []byte `json:"frame"`
}

// NewRedisFanout constructs a Redis-backed fanout.
func NewRedisFanout(rdb *redis.Client, log *slog.Logger) *RedisFanout {
	ctx, cancel := context.WithCancel(context.Background())
	return &RedisFanout{
		rdb:     rdb,
		channel: "signaling:fanout",
		log:     log,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (f *RedisFanout) Publish(room string, targetRole Role, frame []byte) {
	payload, err := json.Marshal(fanoutMessage{Room: room, TargetRole: targetRole, Frame: frame})
	if err != nil {
		return
	}
	if err := f.rdb.Publish(f.ctx, f.channel, payload).Err(); err != nil && f.log != nil {
		f.log.Warn("signaling fanout publish failed", "err", err)
	}
}

func (f *RedisFanout) Start(deliver func(string, Role, []byte)) {
	f.pubsub = f.rdb.Subscribe(f.ctx, f.channel)
	ch := f.pubsub.Channel()
	go func() {
		for {
			select {
			case <-f.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var m fanoutMessage
				if err := json.Unmarshal([]byte(msg.Payload), &m); err != nil {
					continue
				}
				deliver(m.Room, m.TargetRole, m.Frame)
			}
		}
	}()
}

func (f *RedisFanout) Close() error {
	f.cancel()
	if f.pubsub != nil {
		return f.pubsub.Close()
	}
	return nil
}

// RedisPresence shares room membership across instances using a Redis hash of
// role -> connection count, so any instance can tell when both peers of a
// session are connected.
//
// A per-role COUNT (not a set/boolean) is essential for correctness during a
// reconnect: if a role's old socket is still shutting down on one instance
// when a new socket for the same role joins (possibly on a different
// instance), a boolean membership model would have the old socket's eventual
// Leave erase the role's presence entirely — even though the new connection is
// still live. Worse, if that also happened to be the last role removed, it
// would incorrectly report the room "empty" and could cause a still-active
// session to be marked ended (see Hub.OnRoomEnded / session.Service.MarkEnded).
// Counting mirrors LocalPresence's in-memory semantics exactly, just shared via
// Redis instead of a process-local map.
type RedisPresence struct {
	rdb *redis.Client
	ttl time.Duration
	ctx context.Context
}

// NewRedisPresence constructs a Redis-backed presence tracker. ttl bounds how long
// stale membership lingers if an instance dies without cleaning up.
func NewRedisPresence(rdb *redis.Client, ttl time.Duration) *RedisPresence {
	return &RedisPresence{rdb: rdb, ttl: ttl, ctx: context.Background()}
}

func presenceKey(room string) string { return "signaling:presence:" + room }

func (p *RedisPresence) Join(room string, role Role) []Role {
	key := presenceKey(room)
	pipe := p.rdb.TxPipeline()
	pipe.HIncrBy(p.ctx, key, string(role), 1)
	pipe.Expire(p.ctx, key, p.ttl)
	all := pipe.HGetAll(p.ctx, key)
	if _, err := pipe.Exec(p.ctx); err != nil {
		return nil
	}
	var others []Role
	for field, val := range all.Val() {
		if field == string(role) {
			continue
		}
		if n, _ := strconv.Atoi(val); n > 0 {
			others = append(others, Role(field))
		}
	}
	return others
}

// Leave decrements role's count and reports whether every role's count in the
// room is now <=0, atomically (via a pipeline) so a concurrent Join on another
// instance can't be missed between the decrement and the check. Stale
// zero/negative fields are left in place rather than deleted — deleting them
// would need a separate, non-atomic round trip that could race a concurrent
// Join and wipe out its increment; the hash's own TTL (refreshed on Join)
// cleans up an abandoned room eventually.
//
// On a Redis error this conservatively reports not-empty: a false negative
// here just delays the "room ended" signal, whereas a false positive would
// mark a session ended while a peer might still be connected elsewhere.
func (p *RedisPresence) Leave(room string, role Role) bool {
	key := presenceKey(room)
	pipe := p.rdb.TxPipeline()
	pipe.HIncrBy(p.ctx, key, string(role), -1)
	all := pipe.HGetAll(p.ctx, key)
	if _, err := pipe.Exec(p.ctx); err != nil {
		return false
	}
	for _, val := range all.Val() {
		if n, _ := strconv.Atoi(val); n > 0 {
			return false
		}
	}
	return true
}

func (p *RedisPresence) Close() error { return nil }
