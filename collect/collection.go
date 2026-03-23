package collect

import (
	"math/rand"
	"reflect"
	"sort"
	"time"
)

// Collection is a generic wrapper around a slice to provide fluent chainable methods.
type Collection[T any] struct {
	items []T
}

// Pair represents a tuple of two different types, useful for Zip.
type Pair[T, R any] struct {
	First  T
	Second R
}

// Collect is the entry point for creating a generic Collection.
func Collect[T any](items []T) Collection[T] {
	return Collection[T]{items: items}
}

// =========================================================================
// Transformation Methods (Returns Collection[T] for chaining)
// =========================================================================

func (c Collection[T]) Filter(fn func(T) bool) Collection[T] {
	var results []T
	for _, item := range c.items {
		if fn(item) {
			results = append(results, item)
		}
	}
	return Collect(results)
}

func (c Collection[T]) Reject(fn func(T) bool) Collection[T] {
	var results []T
	for _, item := range c.items {
		if !fn(item) {
			results = append(results, item)
		}
	}
	return Collect(results)
}

func (c Collection[T]) Reverse() Collection[T] {
	results := make([]T, len(c.items))
	for i, j := 0, len(c.items)-1; i < len(c.items); i, j = i+1, j-1 {
		results[i] = c.items[j]
	}
	return Collect(results)
}

func (c Collection[T]) Unique() Collection[T] {
	// Without comparable constraint, we use reflection (or slow slice scan).
	// For performance and safety in Go without a comparable constraint,
	// we do a linear scan with reflect.DeepEqual.
	// (In production, a KeyedUnique with a callback is faster, but this matches Laravel's magic).
	var results []T
	for _, item := range c.items {
		exists := false
		for _, r := range results {
			if reflect.DeepEqual(item, r) {
				exists = true
				break
			}
		}
		if !exists {
			results = append(results, item)
		}
	}
	return Collect(results)
}

func (c Collection[T]) Shuffle() Collection[T] {
	results := make([]T, len(c.items))
	copy(results, c.items)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(results), func(i, j int) {
		results[i], results[j] = results[j], results[i]
	})
	return Collect(results)
}

func (c Collection[T]) Chunk(size int) []Collection[T] {
	if size <= 0 {
		return nil
	}
	var chunks []Collection[T]
	for i := 0; i < len(c.items); i += size {
		end := i + size
		if end > len(c.items) {
			end = len(c.items)
		}
		chunks = append(chunks, Collect(c.items[i:end]))
	}
	return chunks
}

// Flatten flattens one level deep using reflection, resolving to Collection[any].
func (c Collection[T]) Flatten() Collection[any] {
	var results []any
	for _, item := range c.items {
		val := reflect.ValueOf(item)
		if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
			for i := 0; i < val.Len(); i++ {
				results = append(results, val.Index(i).Interface())
			}
		} else {
			results = append(results, item)
		}
	}
	return Collect(results)
}

func (c Collection[T]) Compact() Collection[T] {
	var results []T
	for _, item := range c.items {
		if !reflect.ValueOf(item).IsZero() {
			results = append(results, item)
		}
	}
	return Collect(results)
}

func (c Collection[T]) Take(n int) Collection[T] {
	if n < 0 {
		return c.TakeLast(-n)
	}
	if n > len(c.items) {
		n = len(c.items)
	}
	return Collect(c.items[:n])
}

func (c Collection[T]) TakeLast(n int) Collection[T] {
	if n > len(c.items) {
		n = len(c.items)
	}
	return Collect(c.items[len(c.items)-n:])
}

func (c Collection[T]) Skip(n int) Collection[T] {
	if n > len(c.items) {
		n = len(c.items)
	}
	return Collect(c.items[n:])
}

func (c Collection[T]) SkipLast(n int) Collection[T] {
	if n > len(c.items) {
		n = len(c.items)
	}
	return Collect(c.items[:len(c.items)-n])
}

func (c Collection[T]) Slice(start, end int) Collection[T] {
	if start < 0 {
		start = 0
	}
	if end > len(c.items) {
		end = len(c.items)
	}
	if start > end {
		start = end
	}
	return Collect(c.items[start:end])
}

func (c Collection[T]) Sort(fn func(a, b T) bool) Collection[T] {
	results := make([]T, len(c.items))
	copy(results, c.items)
	sort.SliceStable(results, func(i, j int) bool {
		return fn(results[i], results[j])
	})
	return Collect(results)
}

func (c Collection[T]) SortDesc(fn func(a, b T) bool) Collection[T] {
	results := make([]T, len(c.items))
	copy(results, c.items)
	sort.SliceStable(results, func(i, j int) bool {
		return fn(results[j], results[i]) // inverted
	})
	return Collect(results)
}

func (c Collection[T]) Append(items ...T) Collection[T] {
	return Collect(append(c.items, items...))
}

func (c Collection[T]) Prepend(items ...T) Collection[T] {
	return Collect(append(items, c.items...))
}

func (c Collection[T]) Tap(fn func(Collection[T])) Collection[T] {
	fn(c)
	return c
}

// =========================================================================
// Free Generic Mappers (Returns different types)
// =========================================================================

func MapCollect[T, R any](c Collection[T], fn func(T) R) Collection[R] {
	var results []R
	for _, item := range c.items {
		results = append(results, fn(item))
	}
	return Collect(results)
}

func FlatMapCollect[T, R any](c Collection[T], fn func(T) []R) Collection[R] {
	var results []R
	for _, item := range c.items {
		results = append(results, fn(item)...)
	}
	return Collect(results)
}

func GroupByCollect[T any, K comparable](c Collection[T], fn func(T) K) map[K]Collection[T] {
	groups := make(map[K][]T)
	for _, item := range c.items {
		k := fn(item)
		groups[k] = append(groups[k], item)
	}
	result := make(map[K]Collection[T])
	for k, v := range groups {
		result[k] = Collect(v)
	}
	return result
}

func KeyByCollect[T any, K comparable](c Collection[T], fn func(T) K) map[K]T {
	result := make(map[K]T)
	for _, item := range c.items {
		result[fn(item)] = item
	}
	return result
}

func ZipCollect[T, R any](a Collection[T], b Collection[R]) Collection[Pair[T, R]] {
	length := len(a.items)
	if len(b.items) < length {
		length = len(b.items)
	}
	var results []Pair[T, R]
	for i := 0; i < length; i++ {
		results = append(results, Pair[T, R]{First: a.items[i], Second: b.items[i]})
	}
	return Collect(results)
}

// =========================================================================
// Terminal Aggregation Methods
// =========================================================================

func (c Collection[T]) Count() int {
	return len(c.items)
}

func (c Collection[T]) IsEmpty() bool {
	return len(c.items) == 0
}

func (c Collection[T]) IsNotEmpty() bool {
	return len(c.items) > 0
}

func (c Collection[T]) First() (T, bool) {
	if len(c.items) == 0 {
		var zero T
		return zero, false
	}
	return c.items[0], true
}

func (c Collection[T]) Last() (T, bool) {
	if len(c.items) == 0 {
		var zero T
		return zero, false
	}
	return c.items[len(c.items)-1], true
}

func (c Collection[T]) Nth(n int) (T, bool) {
	if n < 0 || n >= len(c.items) {
		var zero T
		return zero, false
	}
	return c.items[n], true
}

func (c Collection[T]) Random() (T, bool) {
	if len(c.items) == 0 {
		var zero T
		return zero, false
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return c.items[rng.Intn(len(c.items))], true
}

func (c Collection[T]) Contains(fn func(T) bool) bool {
	for _, item := range c.items {
		if fn(item) {
			return true
		}
	}
	return false
}

func (c Collection[T]) Every(fn func(T) bool) bool {
	if len(c.items) == 0 {
		return true
	}
	for _, item := range c.items {
		if !fn(item) {
			return false
		}
	}
	return true
}

func (c Collection[T]) Some(fn func(T) bool) bool {
	return c.Contains(fn)
}

func (c Collection[T]) Sum(fn func(T) float64) float64 {
	var sum float64
	for _, item := range c.items {
		sum += fn(item)
	}
	return sum
}

func (c Collection[T]) Min(fn func(T) float64) float64 {
	if len(c.items) == 0 {
		return 0
	}
	min := fn(c.items[0])
	for _, item := range c.items[1:] {
		val := fn(item)
		if val < min {
			min = val
		}
	}
	return min
}

func (c Collection[T]) Max(fn func(T) float64) float64 {
	if len(c.items) == 0 {
		return 0
	}
	max := fn(c.items[0])
	for _, item := range c.items[1:] {
		val := fn(item)
		if val > max {
			max = val
		}
	}
	return max
}

func (c Collection[T]) Avg(fn func(T) float64) float64 {
	if len(c.items) == 0 {
		return 0
	}
	return c.Sum(fn) / float64(len(c.items))
}

func (c Collection[T]) ToSlice() []T {
	return c.items
}

func (c Collection[T]) Each(fn func(int, T)) {
	for i, item := range c.items {
		fn(i, item)
	}
}
