# Metrics

A dependency-free, Prometheus-format metrics package: counters, gauges,
histograms, and an exposition handler.

## Instruments

```go
var ordersPlaced = metrics.NewCounter("orders_placed_total", "Orders placed")
var queueDepth   = metrics.NewGauge("queue_depth", "Jobs waiting")
var latency      = metrics.NewHistogram("request_seconds", "Request latency",
    []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 5})
```

| Type | Methods |
| --- | --- |
| `Counter` | `Inc(Labels)`, `Add(delta uint64, Labels)` — monotonic |
| `Gauge` | `Set(int64, Labels)`, `Inc`, `Dec`, `Add(delta int64, Labels)` — goes up and down |
| `Histogram` | `Observe(float64, Labels)` — distribution over the given buckets |

`Labels` is `map[string]string`:

```go
ordersPlaced.Inc(metrics.Labels{"channel": "web"})
latency.Observe(elapsed.Seconds(), metrics.Labels{"route": "/checkout"})
```

**Keep label cardinality low.** A label whose value is a user id, order id, or
raw URL path creates one time series per value and will eventually exhaust
memory. Use the route pattern (`/users/:id`), never the resolved path.

## Exposition

```go
app.Router.Mount("/metrics", metrics.Handler())
```

`Handler()` serves the default registry. `RegistryHandler(r)` serves a specific
`Registry` — useful when you want an isolated set for tests. A `Registry` has
`Register(m)` and `Expose() string`.

## Request metrics

`middleware.Metrics()` records per-request counters and latency automatically.
Add it globally:

```go
app.Router.Use(middleware.Metrics())
```

## Runtime statistics

`ReadRuntimeStats() RuntimeStats` snapshots Go runtime numbers — goroutines,
heap, GC — for a health or debug endpoint.

## Guidance

1. Declare instruments as package-level vars; creating one per request defeats
   aggregation.
2. Counters only ever go up — use a gauge for anything that can decrease.
3. Choose histogram buckets from the latency you actually care about; the
   defaults are rarely right for your service.
4. `/metrics` should not be publicly reachable — mount it behind auth or bind it
   to an internal port.
