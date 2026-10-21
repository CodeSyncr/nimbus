package testing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/queue"
	"github.com/CodeSyncr/nimbus/router"
	nimbustesting "github.com/CodeSyncr/nimbus/testing"
)

type sendWelcome struct {
	Email string `json:"email"`
	ran   *int
}

func (j *sendWelcome) Handle(context.Context) error {
	if j.ran != nil {
		*j.ran++
	}
	if j.Email == "" {
		return errors.New("no email")
	}
	return nil
}

type unmarshalable struct {
	Ch chan int `json:"ch"`
}

func (unmarshalable) Handle(context.Context) error { return nil }

func TestFakeJobsCapturesDispatchFromHandler(t *testing.T) {
	jobs := nimbustesting.FakeJobs(t)

	r := router.New()
	r.Post("/signup", func(c *nhttp.Context) error {
		return queue.Dispatch(&sendWelcome{Email: "a@b.co"}).OnQueue("mail").Delay(time.Minute).Dispatch(c.Ctx())
	})
	nimbustesting.NewTestClient(r).PostJSON("/signup", map[string]any{}).AssertOK(t)

	jobs.AssertDispatchedCount(t, 1)
	nimbustesting.AssertDispatched(t, jobs, func(j *sendWelcome) bool { return j.Email == "a@b.co" })
	nimbustesting.AssertNotDispatched(t, jobs, func(j *sendWelcome) bool { return j.Email == "x@y.z" })

	d := jobs.Dispatched()[0]
	if d.Queue != "mail" || d.Delay != time.Minute {
		t.Fatalf("queue/delay = %q/%s, want mail/1m", d.Queue, d.Delay)
	}
	if d.Job != nil {
		t.Fatalf("unregistered job decoded to %T", d.Job)
	}
	if errs := jobs.ProcessAll(context.Background()); len(errs) != 1 {
		t.Fatalf("ProcessAll of an unregistered job: errs = %v, want one", errs)
	}
}

func TestFakeJobsRegisteredJobsRun(t *testing.T) {
	jobs := nimbustesting.FakeJobs(t)
	queue.Register(&sendWelcome{})

	if err := queue.Dispatch(&sendWelcome{Email: "a@b.co"}).Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := queue.Dispatch(&sendWelcome{}).Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := jobs.Dispatched()[0].Job.(*sendWelcome); !ok {
		t.Fatalf("registered job not decoded: %#v", jobs.Dispatched()[0])
	}
	if errs := jobs.ProcessAll(context.Background()); len(errs) != 1 || errs[0].Error() != "no email" {
		t.Fatalf("errs = %v, want [no email]", errs)
	}
	if n, _ := queue.GetGlobal().Adapter().Len(context.Background(), "default"); n != 2 {
		t.Fatalf("Len(default) = %d, want 2", n)
	}
}

func TestFakeJobsRejectsUnmarshalableJob(t *testing.T) {
	jobs := nimbustesting.FakeJobs(t)
	if err := queue.Dispatch(unmarshalable{Ch: make(chan int)}).Dispatch(context.Background()); err == nil {
		t.Fatal("dispatching a job with a channel field succeeded")
	}
	jobs.AssertNothingDispatched(t)
}

func TestFakeJobsRestoresPreviousManager(t *testing.T) {
	prev := queue.NewManager(nil)
	queue.SetGlobal(prev)
	t.Cleanup(func() { queue.SetGlobal(nil) })

	t.Run("faked", func(t *testing.T) {
		nimbustesting.FakeJobs(t)
		if queue.GetGlobal() == prev {
			t.Fatal("FakeJobs did not replace the global manager")
		}
	})
	if queue.GetGlobal() != prev {
		t.Fatal("global manager not restored after the test")
	}
}

func TestFakeQueueManualPushStillWorks(t *testing.T) {
	ran := 0
	q := nimbustesting.NewFakeQueue()
	q.PushToQueue("mail", &sendWelcome{Email: "a@b.co", ran: &ran})
	if got := nimbustesting.JobsOf[*sendWelcome](q); len(got) != 1 || got[0].Email != "a@b.co" {
		t.Fatalf("JobsOf = %v", got)
	}
	if errs := q.ProcessAll(context.Background()); len(errs) != 0 || ran != 1 {
		t.Fatalf("errs = %v, ran = %d", errs, ran)
	}
}

func TestFakeClaimFailsOverlappingTests(t *testing.T) {
	nimbustesting.FakeJobs(t)
	ft := &fatalRecorder{TB: t}
	func() {
		defer func() { _ = recover() }()
		nimbustesting.FakeJobs(ft)
	}()
	if !ft.fatal {
		t.Fatal("second FakeJobs while the first is active did not fail")
	}
}

// fatalRecorder records Fatalf instead of stopping the real test.
type fatalRecorder struct {
	testing.TB
	fatal bool
}

func (f *fatalRecorder) Fatalf(string, ...any) { f.fatal = true; panic("fatal") }
func (f *fatalRecorder) Name() string          { return "other" }
