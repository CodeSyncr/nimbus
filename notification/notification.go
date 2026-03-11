package notification

import (
	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/plugins/transmit"
)

// Notification describes a multi-channel notification.
// Implementations can choose to support one or more channels.
type Notification interface {
	// ToMail returns the mail message for this notification, or nil to skip mail.
	ToMail() *mail.Message
	// ToBroadcast returns the channel and payload for realtime broadcasts.
	// Return empty channel to skip broadcast.
	ToBroadcast() (channel string, payload any)
}

// Send delivers the notification on all supported channels.
// If mail.Default is nil, the mail channel is skipped.
func Send(n Notification) error {
	if err := SendMail(n); err != nil {
		return err
	}
	Broadcast(n)
	return nil
}

// SendMail sends the notification via the configured mail driver (if any).
func SendMail(n Notification) error {
	msg := n.ToMail()
	if msg == nil {
		return nil
	}
	return mail.Send(msg)
}

// Broadcast sends the notification over the transmit SSE system (if configured).
// When no Transmit plugin/transport is registered, this is a no-op.
func Broadcast(n Notification) {
	channel, payload := n.ToBroadcast()
	if channel == "" {
		return
	}
	transmit.Broadcast(channel, payload)
}

