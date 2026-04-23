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
	if payload == nil {
		return
	}
	observerMu.RLock()
	list := append([]Observer(nil), observers...)
	observerMu.RUnlock()
	for _, o := range list {
		if ro, ok := o.(RetryObserver); ok {
			ro.JobRetried(payload, nextDelay)
		}
	}
}

func notifyReclaimed(queue string, count int) {
	if count <= 0 {
		return
	}
	observerMu.RLock()
	list := append([]Observer(nil), observers...)
	observerMu.RUnlock()
	for _, o := range list {
		if ro, ok := o.(ReclaimObserver); ok {
			ro.JobsReclaimed(queue, count)
		}
	}
}
