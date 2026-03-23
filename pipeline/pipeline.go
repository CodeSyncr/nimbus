package pipeline

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Sequential runs jobs sequentially and stops on the first error.
func Sequential[T any](items []T, fn func(T) error) error {
	for _, item := range items {
		if err := fn(item); err != nil {
			return err
		}
	}
	return nil
}

// Parallel runs jobs concurrently without limits and collects all errors.
func Parallel[T any](items []T, fn func(T) error) []error {
	if len(items) == 0 {
		return nil
	}

	errs := make([]error, len(items))
	var wg sync.WaitGroup
	wg.Add(len(items))

	for i, item := range items {
		go func(i int, val T) {
			defer wg.Done()
			errs[i] = fn(val)
		}(i, item)
	}

	wg.Wait()

	// Filter out nil errors
	var actualErrs []error
	for _, err := range errs {
		if err != nil {
			actualErrs = append(actualErrs, err)
		}
	}
	return actualErrs
}

// ParallelMap maps items concurrently while preserving order, returning elements and collected errors.
func ParallelMap[T any, R any](items []T, fn func(T) (R, error)) ([]R, []error) {
	if len(items) == 0 {
		return nil, nil
	}

	results := make([]R, len(items))
	errs := make([]error, len(items))
	var wg sync.WaitGroup
	wg.Add(len(items))

	for i, item := range items {
		go func(i int, val T) {
			defer wg.Done()
			res, err := fn(val)
			results[i] = res
			errs[i] = err
		}(i, item)
	}

	wg.Wait()

	var actualErrs []error
	for _, err := range errs {
		if err != nil {
			actualErrs = append(actualErrs, err)
		}
	}
	return results, actualErrs
}

// Pool runs jobs concurrently but limits the number of active goroutines to `concurrency`.
func Pool[T any](items []T, concurrency int, fn func(T) error) []error {
	if len(items) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	errs := make([]error, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency) // Semaphore channel

	wg.Add(len(items))
	for i, item := range items {
		sem <- struct{}{} // acquire
		go func(i int, val T) {
			defer wg.Done()
			defer func() { <-sem }() // release

			errs[i] = fn(val)
		}(i, item)
	}

	wg.Wait()

	var actualErrs []error
	for _, err := range errs {
		if err != nil {
			actualErrs = append(actualErrs, err)
		}
	}
	return actualErrs
}

// Retry retries a function a given number of times with a specified delay between attempts.
func Retry(attempts int, delay time.Duration, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i < attempts-1 && delay > 0 {
			time.Sleep(delay)
		}
	}
	return err
}

// WithTimeout runs a function with a timeout, returning an error if it takes too long.
func WithTimeout[T any](d time.Duration, fn func() (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()

	type result struct {
		val T
		err error
	}

	ch := make(chan result, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// We recover so the parent context selects the timeout/doesn't crash randomly
				var zero T
				ch <- result{val: zero, err: errors.New("panic inside WithTimeout")}
			}
		}()
		val, err := fn()
		ch <- result{val, err}
	}()

	select {
	case res := <-ch:
		return res.val, res.err
	case <-ctx.Done():
		var zero T
		return zero, context.DeadlineExceeded
	}
}
