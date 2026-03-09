/*
|--------------------------------------------------------------------------
| Transmit Public API
|--------------------------------------------------------------------------
|
| Broadcast and BroadcastExcept for server-to-client push.
|
|   transmit.Broadcast("notifications", map[string]any{"msg": "Hello"})
|   transmit.BroadcastExcept("chats/1", data, senderUID)
|
*/

package transmit

import "context"

// Broadcast sends payload to all subscribers of channel.
// With transport configured, publishes to Redis so all instances deliver.
func Broadcast(channel string, payload any) {
	emitBroadcast(channel, payload)
	t := getGlobalTransport()
	if t != nil {
		_ = t.Publish(context.Background(), channel, payload, nil)
		return
	}
	s := GetStore()
	if s != nil {
		s.DeliverToChannel(channel, payload)
	}
}

// BroadcastExcept sends payload to all subscribers except the given UIDs.
func BroadcastExcept(channel string, payload any, excludeUIDs ...string) {
	emitBroadcast(channel, payload)
	t := getGlobalTransport()
	if t != nil {
		_ = t.Publish(context.Background(), channel, payload, excludeUIDs)
		return
	}
	s := GetStore()
	if s != nil {
		s.DeliverToChannel(channel, payload, excludeUIDs...)
	}
}

// GetSubscribers returns UIDs subscribed to channel (AdonisJS getSubscribersFor).
func GetSubscribers(channel string) []string {
	s := GetStore()
	if s == nil {
		return nil
	}
	return s.Subscribers(channel)
}
