/*
|--------------------------------------------------------------------------
| Transmit Lifecycle Hooks
|--------------------------------------------------------------------------
|
| Listen for connect, disconnect, subscribe, unsubscribe, broadcast.
|   transmit.OnConnect(func(uid string) { log.Printf("Client %s connected", uid) })
|
*/

package transmit

import (
	"sync"
)

type (
	ConnectHook     func(uid string)
	DisconnectHook  func(uid string)
	SubscribeHook   func(uid, channel string)
	UnsubscribeHook func(uid, channel string)
	BroadcastHook   func(channel string, payload any)
)

var (
	hooksMu       sync.RWMutex
	onConnect     []ConnectHook
	onDisconnect  []DisconnectHook
	onSubscribe   []SubscribeHook
	onUnsubscribe []UnsubscribeHook
	onBroadcast   []BroadcastHook
)

// OnConnect registers a callback for new SSE connections.
func OnConnect(fn ConnectHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	onConnect = append(onConnect, fn)
}

// OnDisconnect registers a callback when a client disconnects.
func OnDisconnect(fn DisconnectHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	onDisconnect = append(onDisconnect, fn)
}

// OnSubscribe registers a callback when a client subscribes to a channel.
func OnSubscribe(fn SubscribeHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	onSubscribe = append(onSubscribe, fn)
}

// OnUnsubscribe registers a callback when a client unsubscribes.
func OnUnsubscribe(fn UnsubscribeHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	onUnsubscribe = append(onUnsubscribe, fn)
}

// OnBroadcast registers a callback when a message is broadcast.
func OnBroadcast(fn BroadcastHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	onBroadcast = append(onBroadcast, fn)
}

func emitConnect(uid string) {
	hooksMu.RLock()
	fns := onConnect
	hooksMu.RUnlock()
	for _, fn := range fns {
		fn(uid)
	}
}

func emitDisconnect(uid string) {
	hooksMu.RLock()
	fns := onDisconnect
	hooksMu.RUnlock()
	for _, fn := range fns {
		fn(uid)
	}
}

func emitSubscribe(uid, channel string) {
	hooksMu.RLock()
	fns := onSubscribe
	hooksMu.RUnlock()
	for _, fn := range fns {
		fn(uid, channel)
	}
}

func emitUnsubscribe(uid, channel string) {
	hooksMu.RLock()
	fns := onUnsubscribe
	hooksMu.RUnlock()
	for _, fn := range fns {
		fn(uid, channel)
	}
}

func emitBroadcast(channel string, payload any) {
	hooksMu.RLock()
	fns := onBroadcast
	hooksMu.RUnlock()
	for _, fn := range fns {
		fn(channel, payload)
	}
}
