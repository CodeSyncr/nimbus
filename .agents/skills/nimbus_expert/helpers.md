# Helpers — Strings, Collections, Time, Pipeline

Four small utility packages. All are pure Go with no framework dependencies, so
they are safe to use anywhere, including tests and CLI commands.

---

## `str` — fluent strings

`str.Str(s)` returns a `NimbusString` whose methods chain and whose `String()`
unwraps.

```go
slug := str.Str("  Hello, Nimbus World! ").Trim().Slug().String()  // "hello-nimbus-world"
```

**Case conversion:** `Upper`, `Lower`, `Title`, `Pascal`, `Camel`, `Snake`,
`Kebab`, `Slug(separator ...string)`.

**Trimming and editing:** `Trim`, `TrimLeft`, `TrimRight`, `Replace(old, new)`
(first occurrence), `ReplaceAll`, `Append`, `Prepend`, `Repeat(n)`, `Reverse`.

**Truncation:** `Limit(n)` cuts to n characters; `Words(n)` cuts to n words;
`Excerpt(phrase, radius)` returns the text around a phrase.

**Padding:** `Pad`, `PadLeft`, `PadRight` — each takes `(length, pad)`.

**Masking:** `Mask(char, start, length)` — for card numbers, emails, tokens.

```go
str.Str("4111111111111111").Mask("*", 4, 8).String() // "4111********1111"
```

**Inspection:** `Contains`, `StartsWith`, `EndsWith`, `WordCount`, `Length`,
`IsEmpty`, `IsNotEmpty`, `Split(sep) []string`.

`Length()` counts runes, not bytes — it is correct for non-ASCII text.

---

## `collect` — generic collections

`collect.Collect(items)` wraps a slice in a `Collection[T]`. Methods that
return a `Collection` chain; `ToSlice()` unwraps.

```go
active := collect.Collect(users).
    Filter(func(u User) bool { return u.Active }).
    Sort(func(a, b User) bool { return a.Name < b.Name }).
    Take(10).
    ToSlice()
```

**Filtering and shaping:** `Filter`, `Reject`, `Unique`, `Compact` (drops zero
values), `Reverse`, `Shuffle`, `Chunk(size) []Collection[T]`, `Flatten`.

**Slicing:** `Take(n)`, `TakeLast(n)`, `Skip(n)`, `SkipLast(n)`,
`Slice(start, end)`.

**Ordering:** `Sort(less)`, `SortDesc(less)`.

**Building:** `Append(items...)`, `Prepend(items...)`, `Tap(fn)` (peek at the
collection mid-chain without breaking it).

**Reading:** `Count`, `IsEmpty`, `IsNotEmpty`, `First`, `Last`, `Nth(n)`,
`Random` — each accessor returns `(T, bool)` so an empty collection is not a
panic.

**Predicates:** `Contains(fn)`, `Every(fn)`, `Some(fn)`.

**Aggregation:** `Sum(fn)`, `Min(fn)`, `Max(fn)`, `Avg(fn)` — each takes a
`func(T) float64` selector.

**Iteration:** `Each(func(i int, v T))`, `ToSlice()`.

### Type-changing operations

Go methods cannot introduce a new type parameter, so anything that changes the
element type is a **package-level function**:

| Function | Purpose |
| --- | --- |
| `MapCollect(c, fn func(T) R) Collection[R]` | Map to a new type |
| `FlatMapCollect(c, fn func(T) []R) Collection[R]` | Map and flatten |
| `GroupByCollect(c, fn func(T) K) map[K]Collection[T]` | Group by a key |
| `KeyByCollect(c, fn func(T) K) map[K]T` | Index by a key (last wins) |
| `ZipCollect(a, b) Collection[Pair[T, R]]` | Pair two collections elementwise |

```go
names := collect.MapCollect(users, func(u User) string { return u.Name })
byRole := collect.GroupByCollect(users, func(u User) string { return u.Role })
```

---

## `timex` — fluent time

`timex.Now()`, `timex.Parse(s)`, `timex.FromTime(t)` produce a `NimbusTime`.

**Arithmetic:** `AddDays`/`SubDays`, `AddHours`/`SubHours`,
`AddMinutes`/`SubMinutes`, `AddMonths`/`SubMonths`, `AddYears`/`SubYears`.

**Boundaries:** `StartOfDay`, `EndOfDay`, `StartOfWeek`, `EndOfWeek`,
`StartOfMonth`, `EndOfMonth`, `StartOfYear`, `EndOfYear`.

**Comparison:** `IsBefore`, `IsAfter`, `IsSame`, `IsBetween(start, end)`,
`IsToday`, `IsPast`, `IsFuture`, `IsWeekend`, `IsWeekday`.

**Formatting:** `Format(layout)`, `ToDateString`, `ToTimeString`,
`ToDateTimeString`, `ToISO`, `DiffForHumans` ("3 hours ago").

**Differences:** `DiffInDays(other)`, `DiffInHours(other)`.

**Unwrapping:** `Time() time.Time`, `Unix() int64`.

### `timex.Time` — the JSON-safe model field

Distinct from `NimbusTime`. `timex.Time` embeds `time.Time` and provides
`MarshalJSON`/`UnmarshalJSON` that:

- always emit RFC 3339 strings,
- **reject** bare Unix numbers on decode,
- accept `null`,
- reject year 10000+ (the Y2038-adjacent overflow class).

Use it for model fields that cross a JSON boundary; `New(t)` wraps a
`time.Time` and `IsZero()` tests emptiness.

---

## `pipeline` — concurrency helpers

```go
err  := pipeline.Sequential(items, process)          // stop at first error
errs := pipeline.Parallel(items, process)            // one goroutine each
errs := pipeline.Pool(items, 8, process)             // bounded concurrency
res, errs := pipeline.ParallelMap(items, transform)  // parallel with results
```

| Function | Behaviour |
| --- | --- |
| `Sequential(items, fn) error` | In order; returns the first error and stops |
| `Parallel(items, fn) []error` | Unbounded goroutines; returns every error |
| `Pool(items, concurrency, fn) []error` | Bounded worker pool — the safe default |
| `ParallelMap(items, fn) ([]R, []error)` | Parallel map with results and errors |
| `Retry(attempts, delay, fn) error` | Retries a fixed number of times |
| `WithTimeout(d, fn) (T, error)` | Bounds a single operation |

**Prefer `Pool` over `Parallel`.** `Parallel` starts one goroutine per item, so
a large slice of database or HTTP calls will exhaust connections. `Retry` uses a
fixed delay, not exponential backoff — for a remote service under load, wrap it
yourself or you will amplify the outage.
