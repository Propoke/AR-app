package signaling

// Fanout distributes signaling frames to the peers of a room. The default
// LocalFanout delivers in-process; RedisFanout (see redis.go) adds cross-instance
// delivery via pub/sub so the backend can run multiple replicas.
type Fanout interface {
	// Publish delivers frame to the peer of targetRole in room, on every instance.
	Publish(room string, targetRole Role, frame []byte)
	// Start registers the delivery callback invoked for each received frame. It is
	// called once by the Hub during construction.
	Start(deliver func(room string, targetRole Role, frame []byte))
	// Close releases any resources (e.g. the Redis subscription).
	Close() error
}

// LocalFanout delivers frames synchronously within a single process.
type LocalFanout struct {
	deliver func(string, Role, []byte)
}

// NewLocalFanout constructs an in-process fanout.
func NewLocalFanout() *LocalFanout { return &LocalFanout{} }

func (f *LocalFanout) Start(deliver func(string, Role, []byte)) { f.deliver = deliver }

func (f *LocalFanout) Publish(room string, targetRole Role, frame []byte) {
	if f.deliver != nil {
		f.deliver(room, targetRole, frame)
	}
}

func (f *LocalFanout) Close() error { return nil }

// Presence tracks which roles are connected in a room. LocalPresence is in-memory;
// RedisPresence shares state across instances.
type Presence interface {
	// Join records that role is present in room and returns the other roles already
	// present (so the caller can decide whether the room is ready).
	Join(room string, role Role) []Role
	// Leave records that role has left room.
	Leave(room string, role Role)
	Close() error
}
