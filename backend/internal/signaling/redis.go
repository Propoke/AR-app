package signaling

import (
	"context"
	"encoding/json"
	"log/slog"
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

// RedisPresence shares room membership across instances using Redis sets, so any
// instance can tell when both peers of a session are connected.
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
	pipe.SAdd(p.ctx, key, string(role))
	pipe.Expire(p.ctx, key, p.ttl)
	members := pipe.SMembers(p.ctx, key)
	if _, err := pipe.Exec(p.ctx); err != nil {
		return nil
	}
	var others []Role
	for _, m := range members.Val() {
		if Role(m) != role {
			others = append(others, Role(m))
		}
	}
	return others
}

func (p *RedisPresence) Leave(room string, role Role) {
	_ = p.rdb.SRem(p.ctx, presenceKey(room), string(role)).Err()
}

func (p *RedisPresence) Close() error { return nil }
