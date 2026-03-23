package queue

import "time"

// RetryObserver can observe retry scheduling events.
type RetryObserver interface {
	JobRetried(payload *JobPayload, nextDelay time.Duration)
}

// ReclaimObserver can observe reclaimed in-flight jobs.
type ReclaimObserver interface {
	JobsReclaimed(queue string, count int)
}

func notifyRetried(payload *JobPayload, nextDelay time.Duration) {
	o := getObserver()
	ro, ok := o.(RetryObserver)
	if !ok || payload == nil {
		return
	}
	ro.JobRetried(payload, nextDelay)
}

func notifyReclaimed(queue string, count int) {
	if count <= 0 {
		return
	}
	o := getObserver()
	ro, ok := o.(ReclaimObserver)
	if !ok {
		return
	}
	ro.JobsReclaimed(queue, count)
}
