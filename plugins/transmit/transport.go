/*
|--------------------------------------------------------------------------
| Transmit Transport
|--------------------------------------------------------------------------
|
| Multi-instance synchronization. When multiple Nimbus instances run behind
| a load balancer, broadcasts on one instance must reach clients on others.
| Transport publishes to a message bus; all instances subscribe and deliver
| to their local subscribers.
|
|   Config: Transport: transmit.NewRedisTransport(transmit.RedisTransportConfig{})
|   Env: TRANSMIT_TRANSPORT=redis, REDIS_URL, TRANSMIT_REDIS_CHANNEL (optional)
|
*/

package transmit

import "context"

// Transport synchronizes broadcasts across server instances.
type Transport interface {
	// Publish sends channel+payload to all instances (including self via subscription).
	Publish(ctx context.Context, channel string, payload any, excludeUIDs []string) error
	// Subscribe starts receiving published messages. Call from Boot. Blocking.
	Subscribe(ctx context.Context, onMessage func(channel string, payload any, excludeUIDs []string)) error
	// Close stops the transport.
	Close() error
}
