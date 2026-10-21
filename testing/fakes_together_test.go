package testing_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/events"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/notification"
	"github.com/CodeSyncr/nimbus/queue"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/storage"
	nimbustesting "github.com/CodeSyncr/nimbus/testing"
)

// signupRoutes is a handler that touches every faked service, the way real
// application code does: through the package helpers, with no test hooks.
func signupRoutes(disk storage.Driver) *router.Router {
	r := router.New()
	r.Post("/signup", func(c *nhttp.Context) error {
		email := "a@b.co"

		resp, err := http.Post("https://crm.test/v1/contacts", "application/json", strings.NewReader(`{"email":"`+email+`"}`))
		if err != nil {
			return err
		}
		resp.Body.Close()

		if err := disk.Put("avatars/"+email+".png", strings.NewReader("png")); err != nil {
			return err
		}
		if err := cache.Set("user:"+email, true, time.Hour); err != nil {
			return err
		}
		if err := queue.Dispatch(&sendWelcome{Email: email}).OnQueue("mail").Dispatch(c.Ctx()); err != nil {
			return err
		}
		m := mail.NewMessage("Confirm your email")
		m.To = []string{email}
		if err := mail.Send(m); err != nil {
			return err
		}
		if err := notification.Send(&orderShipped{OrderID: 1}); err != nil {
			return err
		}
		if err := events.Dispatch("test.user.signed_up", email); err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, map[string]string{"email": email})
	})
	return r
}

func TestFakesTogetherThroughTestClient(t *testing.T) {
	api := nimbustesting.FakeHTTP(t)
	api.On("POST", "https://crm.test/v1/contacts").RespondJSON(201, map[string]string{"id": "c_1"})
	disk := nimbustesting.FakeStorage(t)
	store := nimbustesting.FakeCache(t)
	jobs := nimbustesting.FakeJobs(t)
	mails := nimbustesting.FakeMail(t)
	notes := nimbustesting.FakeNotifications(t)
	bus := nimbustesting.FakeEvents(t)

	nimbustesting.NewTestClient(signupRoutes(disk)).
		PostJSON("/signup", map[string]any{}).
		AssertCreated(t).
		AssertJSONPath(t, "email", "a@b.co")

	api.AssertSent(t, func(r nimbustesting.RecordedRequest) bool {
		return r.URL.Host == "crm.test" && strings.Contains(string(r.Body), "a@b.co")
	})
	disk.AssertExists(t, "avatars/a@b.co.png")
	store.AssertHas(t, "user:a@b.co")
	nimbustesting.AssertDispatched(t, jobs, func(j *sendWelcome) bool { return j.Email == "a@b.co" })
	mails.AssertSent(t, func(m *mail.Message) bool { return m.Subject == "Confirm your email" })
	nimbustesting.AssertNotificationSent(t, notes, func(n *orderShipped) bool { return n.OrderID == 1 })
	bus.AssertDispatched(t, "test.user.signed_up", func(p any) bool { return p == "a@b.co" })
}

func TestFakeAssertionsReportMisses(t *testing.T) {
	rec := &errorRecorder{TB: t}
	jobs := nimbustesting.FakeJobs(t)
	mails := nimbustesting.FakeMail(t)
	bus := nimbustesting.FakeEvents(t)
	notes := nimbustesting.FakeNotifications(t)
	disk := nimbustesting.FakeStorage(t)
	store := nimbustesting.FakeCache(t)

	_ = queue.Dispatch(&sendWelcome{Email: "a@b.co"}).Dispatch(t.Context())

	nimbustesting.AssertDispatched(rec, jobs, func(j *sendWelcome) bool { return j.Email == "x" })
	nimbustesting.AssertNotDispatched[*sendWelcome](rec, jobs)
	jobs.AssertNothingDispatched(rec)
	mails.AssertSent(rec)
	bus.AssertDispatched(rec, "never")
	nimbustesting.AssertNotificationSent[*orderShipped](rec, notes)
	disk.AssertExists(rec, "nope")
	store.AssertHas(rec, "nope")

	if len(rec.errors) != 8 {
		t.Fatalf("got %d assertion failures, want 8:\n%s", len(rec.errors), strings.Join(rec.errors, "\n"))
	}
}
