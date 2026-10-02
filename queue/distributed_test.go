package queue

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
)

// cluster simulates several app instances sharing one Redis: each has its
// own Manager and adapter, while locks and batch progress live in Redis.
type cluster struct {
	client   *redis.Client
	managers []*Manager
}

func newCluster(t *testing.T, instances int, register func(m *Manager)) *cluster {
	t.Helper()
	c := &cluster{client: newTestRedis(t)}
	for i := 0; i < instances; i++ {
		a := NewRedisAdapter(c.client)
		m := NewManager(a)
		if register != nil {
			register(m)
		}
		c.managers = append(c.managers, m)
	}
	prevM, prevL, prevB := GetGlobal(), GetLocker(), GetBatchStore()
	SetGlobal(c.managers[0])
	SetLocker(NewRedisLocker(c.client))
	SetBatchStore(NewRedisBatchStore(c.client))
	t.Cleanup(func() {
		SetGlobal(prevM)
		SetLocker(prevL)
		SetBatchStore(prevB)
	})
	return c
}

// work runs workersPer workers on every instance until ctx is done.
func (c *cluster) work(ctx context.Context, workersPer int) *sync.WaitGroup {
	var wg sync.WaitGroup
	for _, m := range c.managers {
		for i := 0; i < workersPer; i++ {
			wg.Add(1)
			go func(m *Manager) {
				defer wg.Done()
				for ctx.Err() == nil {
					_ = m.Process(ctx, "default")
				}
			}(m)
		}
	}
	return &wg
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var batchRuns sync.Map // job ID -> *int32

type countedJob struct {
	Key  string `json:"key"`
	Fail bool   `json:"fail"`
}

func (j *countedJob) Handle(context.Context) error {
	v, _ := batchRuns.LoadOrStore(j.Key, new(int32))
	atomic.AddInt32(v.(*int32), 1)
	if j.Fail {
		return errors.New("boom " + j.Key)
	}
	return nil
}

func runsOf(key string) int32 {
	v, ok := batchRuns.Load(key)
	if !ok {
		return 0
	}
	return atomic.LoadInt32(v.(*int32))
}

func TestDurableBatchAcrossInstances(t *testing.T) {
	c := newCluster(t, 2, func(m *Manager) { m.Register(&countedJob{}) })
	batchRuns.Clear()

	var thens, finallies, catches int32
	var finalPending, finalFailed int32 = -1, -1
	RegisterBatch("import", BatchCallbacks{
		Then:  func(context.Context, *Batch) { atomic.AddInt32(&thens, 1) },
		Catch: func(context.Context, *Batch, error) { atomic.AddInt32(&catches, 1) },
		Finally: func(_ context.Context, b *Batch) {
			atomic.StoreInt32(&finalPending, b.PendingJobs())
			atomic.StoreInt32(&finalFailed, b.FailedJobs())
			atomic.AddInt32(&finallies, 1)
		},
	})

	jobs := []Job{}
	for i := 0; i < 20; i++ {
		jobs = append(jobs, &countedJob{Key: "batch-ok-" + string(rune('a'+i))})
	}
	jobs = append(jobs, &countedJob{Key: "batch-fail", Fail: true})
	b := NewBatch(jobs...).Named("import")
	start := time.Now()
	if err := b.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("durable Dispatch should return once jobs are queued")
	}
	if st, err := FindBatch(context.Background(), b.ID); err != nil || st.Total != 21 || st.Pending != 21 {
		t.Fatalf("batch not persisted before workers ran: %+v %v", st, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := c.work(ctx, 3)
	waitFor(t, 30*time.Second, func() bool { return atomic.LoadInt32(&finallies) > 0 })
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()

	if thens != 0 {
		t.Errorf("Then ran %d times for a batch with a failure", thens)
	}
	if finallies != 1 {
		t.Errorf("Finally ran %d times, want 1", finallies)
	}
	if catches != 1 {
		t.Errorf("Catch ran %d times, want 1", catches)
	}
	if finalPending != 0 || finalFailed != 1 {
		t.Errorf("final state pending=%d failed=%d", finalPending, finalFailed)
	}
	if runsOf("batch-fail") != 4 { // first run + 3 retries
		t.Errorf("failing job ran %d times, want 4", runsOf("batch-fail"))
	}
	for i := 0; i < 20; i++ {
		if n := runsOf("batch-ok-" + string(rune('a'+i))); n != 1 {
			t.Errorf("job %d ran %d times", i, n)
		}
	}
}

func TestDurableBatchNeedsNameForCallbacks(t *testing.T) {
	newCluster(t, 1, nil)
	err := NewBatch(&countedJob{}).Then(func(context.Context, *Batch) {}).Dispatch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Named") {
		t.Fatalf("expected an error asking for a batch name, got %v", err)
	}
}

var chainOrder struct {
	sync.Mutex
	keys []string
}

type orderedJob struct {
	Key  string `json:"key"`
	Fail bool   `json:"fail"`
}

func (j *orderedJob) Handle(context.Context) error {
	chainOrder.Lock()
	chainOrder.keys = append(chainOrder.keys, j.Key)
	chainOrder.Unlock()
	if j.Fail {
		return errors.New("chain link failed")
	}
	return nil
}

func TestAsyncChainRunsInOrderAcrossInstances(t *testing.T) {
	c := newCluster(t, 2, func(m *Manager) { m.Register(&orderedJob{}) })
	chainOrder.Lock()
	chainOrder.keys = nil
	chainOrder.Unlock()

	var failures int32
	err := NewChain(&orderedJob{Key: "1"}, &orderedJob{Key: "2"}, &orderedJob{Key: "3"}).
		Named("ordered").
		OnFailure(func(context.Context, Job, error) { atomic.AddInt32(&failures, 1) }).
		DispatchAsync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wg := c.work(ctx, 2)
	waitFor(t, 10*time.Second, func() bool {
		chainOrder.Lock()
		defer chainOrder.Unlock()
		return len(chainOrder.keys) == 3
	})
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()
	if got := strings.Join(chainOrder.keys, ","); got != "1,2,3" {
		t.Fatalf("chain ran %s, want 1,2,3", got)
	}
	if failures != 0 {
		t.Fatalf("OnFailure ran for a chain that succeeded")
	}
}

type uniqueTestJob struct {
	ID string `json:"id"`
}

func (j *uniqueTestJob) Handle(context.Context) error { return nil }
func (j *uniqueTestJob) UniqueID() string             { return j.ID }
func (j *uniqueTestJob) UniqueFor() time.Duration     { return time.Minute }

func TestDispatchUniqueAcrossInstances(t *testing.T) {
	c := newCluster(t, 2, func(m *Manager) { m.Register(&uniqueTestJob{}) })
	ctx := context.Background()

	// Two "instances" with separate Locker values that share Redis.
	SetLocker(NewRedisLocker(c.client))
	if err := DispatchUnique(ctx, &uniqueTestJob{ID: "42"}); err != nil {
		t.Fatal(err)
	}
	SetGlobal(c.managers[1])
	SetLocker(NewRedisLocker(c.client))
	if err := DispatchUnique(ctx, &uniqueTestJob{ID: "42"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.managers[0].Adapter().Len(ctx, "default"); n != 1 {
		t.Fatalf("queued %d copies of a unique job, want 1", n)
	}
	if !IsUniqueLocked(&uniqueTestJob{ID: "42"}) {
		t.Fatal("unique lock should be held while the job is pending")
	}

	// Once a worker finishes the job, the lock is released.
	if err := c.managers[1].Process(ctx, "default"); err != nil {
		t.Fatal(err)
	}
	if IsUniqueLocked(&uniqueTestJob{ID: "42"}) {
		t.Fatal("unique lock should be released after the job finished")
	}
	if err := DispatchUnique(ctx, &uniqueTestJob{ID: "42"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.managers[0].Adapter().Len(ctx, "default"); n != 1 {
		t.Fatalf("re-dispatch after completion queued %d jobs, want 1", n)
	}
}

var overlapState struct {
	running, maxRunning, runs int32
}

type overlapJob struct {
	N int `json:"n"`
}

func (j *overlapJob) Handle(context.Context) error {
	n := atomic.AddInt32(&overlapState.running, 1)
	for {
		m := atomic.LoadInt32(&overlapState.maxRunning)
		if n <= m || atomic.CompareAndSwapInt32(&overlapState.maxRunning, m, n) {
			break
		}
	}
	time.Sleep(50 * time.Millisecond)
	atomic.AddInt32(&overlapState.running, -1)
	atomic.AddInt32(&overlapState.runs, 1)
	return nil
}

func TestWithoutOverlappingAcrossInstances(t *testing.T) {
	c := newCluster(t, 2, func(m *Manager) { m.Register(&overlapJob{}) })
	overlapState.running, overlapState.maxRunning, overlapState.runs = 0, 0, 0
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		w := NewWithoutOverlapping(&overlapJob{N: i}, "report").ReleaseAfter(20 * time.Millisecond)
		if err := Dispatch(w).Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
	}
	wctx, cancel := context.WithCancel(ctx)
	wg := c.work(wctx, 2)
	waitFor(t, 15*time.Second, func() bool { return atomic.LoadInt32(&overlapState.runs) == 4 })
	cancel()
	wg.Wait()
	if overlapState.maxRunning != 1 {
		t.Fatalf("%d overlapping runs at once, want 1", overlapState.maxRunning)
	}
}

func TestWithoutOverlappingHandleDirectly(t *testing.T) {
	newCluster(t, 1, func(m *Manager) { m.Register(&overlapJob{}) })
	overlapState.running, overlapState.maxRunning, overlapState.runs = 0, 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Separate wrapper instances: the old per-instance mutex let these overlap.
			_ = NewWithoutOverlapping(&overlapJob{}, "same").Handle(context.Background())
		}()
	}
	wg.Wait()
	if overlapState.maxRunning != 1 || overlapState.runs != 3 {
		t.Fatalf("maxRunning=%d runs=%d", overlapState.maxRunning, overlapState.runs)
	}
}

var slowRuns int32

type slowJob struct{}

func (slowJob) Handle(context.Context) error {
	atomic.AddInt32(&slowRuns, 1)
	time.Sleep(600 * time.Millisecond)
	return nil
}

func TestHeartbeatKeepsLongJobFromRunningTwice(t *testing.T) {
	c := newCluster(t, 2, func(m *Manager) { m.Register(&slowJob{}) })
	for _, m := range c.managers {
		m.Adapter().(*RedisAdapter).SetVisibilityTimeout(150 * time.Millisecond)
	}
	atomic.StoreInt32(&slowRuns, 0)
	if err := c.managers[0].Dispatch(&slowJob{}).Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	wg := c.work(ctx, 2)
	wg.Wait()
	if n := atomic.LoadInt32(&slowRuns); n != 1 {
		t.Fatalf("job outlasting its visibility timeout ran %d times, want 1", n)
	}
}

var poisonRuns int32

type poisonJob struct{}

func (poisonJob) Handle(context.Context) error {
	atomic.AddInt32(&poisonRuns, 1)
	return nil
}

func TestJobThatKeepsKillingWorkersGivesUp(t *testing.T) {
	c := newCluster(t, 1, func(m *Manager) { m.Register(&poisonJob{}) })
	m := c.managers[0]
	a := m.Adapter().(*RedisAdapter)
	a.SetVisibilityTimeout(30 * time.Millisecond)
	ctx := context.Background()
	atomic.StoreInt32(&poisonRuns, 0)

	if err := m.Dispatch(&poisonJob{}).Retries(1).Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	// Two deliveries whose workers "crash" (never ack).
	for i := 0; i < 2; i++ {
		if _, err := a.Pop(ctx, "default"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Millisecond)
	}
	err := m.Process(ctx, "default")
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("expected the job to fail for exceeding its retries, got %v", err)
	}
	if poisonRuns != 0 {
		t.Fatalf("job ran %d times after using up its attempts", poisonRuns)
	}
}

type releasingJob struct{}

func (*releasingJob) Handle(context.Context) error { return Release(time.Second) }

func TestReleaseRequeuesWithoutCountingAttempt(t *testing.T) {
	adapter := &stubCompletableAdapter{}
	m := NewManager(adapter)
	m.Register(&releasingJob{})
	adapter.popPayload = &JobPayload{ID: "x", JobName: "releasingJob", Queue: "default", Payload: []byte(`{}`), MaxRetries: 0,
		Meta: map[string]interface{}{metaRedisLeaseToken: "tok"}}
	if err := m.Process(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	if len(adapter.pushes) != 1 || adapter.pushes[0].Attempts != 0 || adapter.pushes[0].Delay != time.Second {
		t.Fatalf("release should requeue once with delay and no attempt: %+v", adapter.pushes)
	}
	if _, ok := adapter.pushes[0].Meta[metaRedisLeaseToken]; ok {
		t.Fatal("requeued job kept the old delivery's lease token")
	}
	if adapter.completes != 1 {
		t.Fatalf("released delivery should be acked, completes=%d", adapter.completes)
	}
}

func TestRedisJobLimiterIsSharedAcrossInstances(t *testing.T) {
	client := newTestRedis(t)
	a := NewRedisJobLimiter(client, 20, 1)
	b := NewRedisJobLimiter(client, 20, 1)
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 10; i++ {
		if err := a.Wait(ctx, "mail"); err != nil {
			t.Fatal(err)
		}
		if err := b.Wait(ctx, "mail"); err != nil {
			t.Fatal(err)
		}
	}
	// 20 jobs at 20/s with burst 1 take ~950ms whichever instance takes them.
	if el := time.Since(start); el < 800*time.Millisecond {
		t.Fatalf("20 jobs across two limiters took %s; the limit is not shared", el)
	}
}
