package queue

// Dispatch enqueues a job using the global manager. It is the primary entry
// point for application code to push work onto a queue.
//
// The returned DispatchBuilder can be used to set options before the job is
// actually queued:
//
//	queue.Dispatch(&jobs.SendEmail{UserID: 12}).
//	    Delay(5 * time.Minute).
//	    Dispatch(ctx)
//
//	queue.Dispatch(&jobs.Report{}).
//	    OnQueue("reports").
//	    Retries(5).
//	    Dispatch(ctx)
//
// If no global manager has been configured via queue.SetGlobal (usually done
// by queue.Boot), Dispatch returns a no-op builder so that calls are safe in
// tests or environments where the queue subsystem is disabled.
func Dispatch(job Job) *DispatchBuilder {
	m := GetGlobal()
	if m == nil {
		return &DispatchBuilder{noop: true}
	}
	return m.Dispatch(job)
}

// Register registers a job type with the global manager. Call at startup,
// typically from a central RegisterQueueJobs function in your application.
//
//	queue.Register(&jobs.SendEmail{})
func Register(job Job) {
	m := GetGlobal()
	if m != nil {
		m.Register(job)
	}
}
