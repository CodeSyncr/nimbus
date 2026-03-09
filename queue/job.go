/*
|--------------------------------------------------------------------------
| Queue Job Interface
|--------------------------------------------------------------------------
|
| Jobs implement Handle(ctx). Use queue.Dispatch(&MyJob{...}) to enqueue.
| Inspired by Laravel/AdonisJS queues.
|
|   queue.Dispatch(&jobs.SendEmail{UserID: 12, Subject: "Welcome"})
|   queue.Dispatch(&jobs.ProcessVideo{ID: 1}).Delay(5 * time.Minute)
|   queue.Dispatch(&jobs.Report{}).OnQueue("reports")
|
*/

package queue

import "context"

// Job is the interface for queue jobs.
type Job interface {
	Handle(ctx context.Context) error
}

// FailedJob is optionally implemented for cleanup when job fails permanently.
type FailedJob interface {
	Job
	Failed(ctx context.Context, err error)
}

// JobFunc adapts a function to Job.
type JobFunc func(ctx context.Context) error

func (f JobFunc) Handle(ctx context.Context) error { return f(ctx) }
