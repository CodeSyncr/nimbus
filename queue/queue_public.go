package queue

// Dispatch enqueues a job using the global manager. Returns a no-op builder if no manager set.
//
//	queue.Dispatch(&jobs.SendEmail{UserID: 12}).Delay(5 * time.Minute).Dispatch(ctx)
//	queue.Dispatch(&jobs.Report{}).OnQueue("reports").Dispatch(ctx)
func Dispatch(job Job) *DispatchBuilder {
	m := GetGlobal()
	if m == nil {
		return &DispatchBuilder{noop: true}
	}
	return m.Dispatch(job)
}

// Register registers a job type with the global manager. Call at startup.
//
//	queue.Register(&jobs.SendEmail{})
func Register(job Job) {
	m := GetGlobal()
	if m != nil {
		m.Register(job)
	}
}
