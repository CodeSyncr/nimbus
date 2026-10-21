package testing_test

import (
	"testing"

	"github.com/CodeSyncr/nimbus/events"
	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/notification"
	nimbustesting "github.com/CodeSyncr/nimbus/testing"
)

func TestFakeEventsRecordsWithoutRunningListeners(t *testing.T) {
	ranCreated, ranAudit := 0, 0
	events.Listen("test.user.created", func(any) error { ranCreated++; return nil })
	events.Listen("test.audit", func(any) error { ranAudit++; return nil })
	t.Cleanup(func() { events.Default.Clear("test.user.created", "test.audit") })

	t.Run("faked", func(t *testing.T) {
		bus := nimbustesting.FakeEvents(t, "test.audit")
		_ = events.Dispatch("test.user.created", "a@b.co")
		_ = events.Dispatch("test.audit", nil)

		bus.AssertDispatched(t, "test.user.created", func(p any) bool { return p == "a@b.co" })
		bus.AssertDispatchedTimes(t, "test.audit", 1)
		bus.AssertNotDispatched(t, "test.other")
		if ranCreated != 0 || ranAudit != 1 {
			t.Fatalf("listeners ran created=%d audit=%d, want 0 and 1", ranCreated, ranAudit)
		}
	})

	_ = events.Dispatch("test.user.created", nil)
	if ranCreated != 1 {
		t.Fatal("listeners did not run again after the faked test ended")
	}
}

func TestFakeEventsOnAppDispatcher(t *testing.T) {
	d := events.New()
	ran := false
	d.Listen("app:booted", func(any) error { ran = true; return nil })
	bus := nimbustesting.FakeEventsOn(t, d)
	d.DispatchAsync("app:booted", nil)
	bus.AssertDispatched(t, "app:booted")
	if ran {
		t.Fatal("listener ran on a faked dispatcher")
	}
	_ = events.Dispatch("test.unrelated", nil)
	bus.AssertNotDispatched(t, "test.unrelated")
}

type orderShipped struct{ OrderID int }

func (n *orderShipped) ToMail() *mail.Message {
	m := mail.NewMessage("Shipped")
	m.To = []string{"a@b.co"}
	return m
}
func (n *orderShipped) ToBroadcast() (string, any) { return "orders", n.OrderID }

func TestFakeNotificationsRecordsInsteadOfMailing(t *testing.T) {
	mails := nimbustesting.FakeMail(t)
	hooked := 0
	notification.AfterSend(func(notification.Notification, error) { hooked++ })

	t.Run("faked", func(t *testing.T) {
		notes := nimbustesting.FakeNotifications(t)
		if err := notification.Send(&orderShipped{OrderID: 7}); err != nil {
			t.Fatal(err)
		}
		notes.AssertSentCount(t, 1)
		nimbustesting.AssertNotificationSent(t, notes, func(n *orderShipped) bool { return n.OrderID == 7 })
		mails.AssertNothingSent(t)
	})

	if err := notification.Send(&orderShipped{OrderID: 8}); err != nil {
		t.Fatal(err)
	}
	mails.AssertSentCount(t, 1)
	if hooked != 2 {
		t.Fatalf("AfterSend ran %d times, want 2", hooked)
	}
}
