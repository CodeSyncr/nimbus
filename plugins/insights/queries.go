package insights

import (
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/lucid"
)

// QueryStat is every run of one query, whatever values it was run with.
type QueryStat struct {
	Shape   string    `json:"shape"` // the query with its values taken out
	Count   int       `json:"count"`
	Errors  int       `json:"errors"`
	AvgMS   float64   `json:"avg_ms"`
	MaxMS   float64   `json:"max_ms"`
	TotalMS float64   `json:"total_ms"`
	Rows    int64     `json:"rows"`   // rows it gave or changed, in all
	Caller  string    `json:"caller"` // where in the app it was last run from
	LastAt  time.Time `json:"last_at"`
}

// SlowQuery is one run of a query that took long, or failed.
type SlowQuery struct {
	At     time.Time `json:"at"`
	SQL    string    `json:"sql"` // as it was run, values and all
	Shape  string    `json:"shape"`
	MS     float64   `json:"ms"`
	Rows   int64     `json:"rows"`
	Caller string    `json:"caller"`
	Error  string    `json:"error,omitempty"`
}

// PoolStat is the database's connections.
type PoolStat struct {
	Open         int     `json:"open"`
	InUse        int     `json:"in_use"`
	Idle         int     `json:"idle"`
	MaxOpen      int     `json:"max_open"`
	WaitCount    int64   `json:"wait_count"` // times a request had to wait for a connection
	WaitMS       float64 `json:"wait_ms"`
	MaxIdleClose int64   `json:"max_idle_closed"`
}

// QueryReport is the database as the app uses it.
type QueryReport struct {
	Total  int         `json:"total"`
	Top    []QueryStat `json:"top"`    // most time spent first
	Slow   []SlowQuery `json:"slow"`   // newest first
	Failed []SlowQuery `json:"failed"` // newest first
	Pool   *PoolStat   `json:"pool,omitempty"`
	Driver string      `json:"driver"`
	SlowMS float64     `json:"slow_ms"`
}

type queryAgg struct {
	count, errors int
	total, max    float64
	rows          int64
	caller        string
	last          time.Time
}

type queryLog struct {
	mu     sync.Mutex
	stats  map[string]*queryAgg
	total  int
	slow   *ring[SlowQuery]
	failed *ring[SlowQuery]
	slowAt time.Duration
	driver string
}

func newQueryLog(keep int, slow time.Duration) *queryLog {
	return &queryLog{stats: map[string]*queryAgg{}, slow: newRing[SlowQuery](keep), failed: newRing[SlowQuery](keep / 2), slowAt: slow}
}

var (
	sqlString = regexp.MustCompile(`'(?:[^']|'')*'`)
	sqlNumber = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	sqlList   = regexp.MustCompile(`\((?:\s*\?\s*,)+\s*\?\s*\)`)
	sqlSpace  = regexp.MustCompile(`\s+`)
)

var sqlQuoted = regexp.MustCompile(`"(?:[^"]|"")*"`)

// Shape is a query with its values taken out, so every run of it is the
// same text: WHERE id = 7 and WHERE id = 9 are both WHERE id = ?.
func Shape(sql string) string { return shapeFor(sql, "") }

// shapeFor is Shape for a driver. SQLite's statements are written out
// with their text values in double quotes (its names are in backticks),
// so there a double-quoted word is a value; elsewhere it is a name.
func shapeFor(sql, driver string) string {
	s := sql
	if strings.HasPrefix(driver, "sqlite") {
		s = sqlQuoted.ReplaceAllString(s, "?")
	}
	s = sqlString.ReplaceAllString(s, "?")
	s = sqlNumber.ReplaceAllString(s, "?")
	s = sqlList.ReplaceAllString(s, "(?…)")
	s = strings.TrimSpace(sqlSpace.ReplaceAllString(s, " "))
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return s
}

// caller is the place in the app's own code a query was run from: the
// first frame that is not the database layer's, the framework's or Go's.
func caller() string {
	var pcs [40]uintptr
	n := runtime.Callers(3, pcs[:])
	it := runtime.CallersFrames(pcs[:n])
	for {
		f, more := it.Next()
		fn := f.Function
		framework := strings.Contains(fn, "CodeSyncr/nimbus/") && !strings.HasSuffix(f.File, "_test.go")
		if fn != "" && !framework && !strings.Contains(f.File, "/pkg/mod/") && !strings.HasPrefix(fn, "runtime.") && !strings.Contains(fn, "gorm.io/") &&
			!strings.HasPrefix(fn, "database/sql") && !strings.Contains(f.File, "/go/src/") && !strings.Contains(f.File, "/toolchain@") &&
			!strings.Contains(f.File, "/libexec/") && !strings.HasPrefix(fn, "net/http.") && !strings.HasPrefix(fn, "testing.") {
			return shortFile(f.File) + ":" + itoa(f.Line)
		}
		if !more {
			return ""
		}
	}
}

// The callbacks are put on a database once, whoever asks, and hand each
// statement to every collector that is running: a second collector on
// the same database (a test's, mostly) does not push the first one's out.
var hooked struct {
	mu   sync.Mutex
	dbs  map[*lucid.DB]bool
	live []*Insights
}

func watch(in *Insights) {
	hooked.mu.Lock()
	hooked.live = append(hooked.live, in)
	hooked.mu.Unlock()
}

func unwatch(in *Insights) {
	hooked.mu.Lock()
	for i, x := range hooked.live {
		if x == in {
			hooked.live = append(hooked.live[:i:i], hooked.live[i+1:]...)
			break
		}
	}
	hooked.mu.Unlock()
}

// hook puts the callbacks on a database, before and after every kind of
// statement. They run on the goroutine that runs the statement, which is
// the goroutine of the request that asked for it: that is how a query is
// known to belong to a request. (The framework's own query events are
// sent on after the fact, from another goroutine, so they cannot say.)
func hook(db *lucid.DB) {
	hooked.mu.Lock()
	if hooked.dbs == nil {
		hooked.dbs = map[*lucid.DB]bool{}
	}
	done := hooked.dbs[db]
	hooked.dbs[db] = true
	hooked.mu.Unlock()
	if done {
		return
	}
	before := func(d *lucid.DB) { d.InstanceSet("insights:start", time.Now()) }
	after := func(d *lucid.DB) {
		if d.Statement == nil {
			return
		}
		sql := d.Statement.SQL.String()
		if sql == "" {
			return
		}
		hooked.mu.Lock()
		live := hooked.live
		hooked.mu.Unlock()
		if len(live) == 0 {
			return
		}
		var took time.Duration
		if v, ok := d.InstanceGet("insights:start"); ok {
			if t, isTime := v.(time.Time); isTime {
				took = time.Since(t)
			}
		}
		qp := database.QueryPayload{SQL: d.Dialector.Explain(sql, d.Statement.Vars...), Vars: d.Statement.Vars, RowsAffected: d.Statement.RowsAffected,
			Duration: took, Error: d.Error, Connection: d.Dialector.Name()}
		for _, in := range live {
			in.queries.record(qp, in.requests)
		}
	}
	cb := db.Callback()
	_ = cb.Query().Before("gorm:query").Register("insights:before_query", before)
	_ = cb.Query().After("gorm:query").Register("insights:after_query", after)
	_ = cb.Create().Before("gorm:create").Register("insights:before_create", before)
	_ = cb.Create().After("gorm:create").Register("insights:after_create", after)
	_ = cb.Update().Before("gorm:update").Register("insights:before_update", before)
	_ = cb.Update().After("gorm:update").Register("insights:after_update", after)
	_ = cb.Delete().Before("gorm:delete").Register("insights:before_delete", before)
	_ = cb.Delete().After("gorm:delete").Register("insights:after_delete", after)
	_ = cb.Row().Before("gorm:row").Register("insights:before_row", before)
	_ = cb.Row().After("gorm:row").Register("insights:after_row", after)
	_ = cb.Raw().Before("gorm:raw").Register("insights:before_raw", before)
	_ = cb.Raw().After("gorm:raw").Register("insights:after_raw", after)
}

func (l *queryLog) record(qp database.QueryPayload, rq *requestLog) {
	ms := float64(qp.Duration.Microseconds()) / 1000
	shape := shapeFor(qp.SQL, qp.Connection)
	now := time.Now()
	slow := qp.Duration >= l.slowAt
	// where from costs a stack walk: only when it will be kept
	from := ""
	l.mu.Lock()
	a := l.stats[shape]
	if a == nil && len(l.stats) < 500 {
		a = &queryAgg{}
		l.stats[shape] = a
	}
	needCaller := a != nil && a.caller == ""
	l.mu.Unlock()
	var t *track
	if rq != nil {
		t = rq.current()
	}
	first := false
	if t != nil {
		if hit := t.shapes[shape]; hit == nil && len(t.shapes) < 200 {
			first = true
		}
	}
	if slow || qp.Error != nil || needCaller || first {
		from = caller()
	}
	l.mu.Lock()
	l.total++
	if l.driver == "" {
		l.driver = qp.Connection
	}
	if a != nil {
		a.count++
		a.total += ms
		if ms > a.max {
			a.max = ms
		}
		a.rows += qp.RowsAffected
		a.last = now
		if from != "" {
			a.caller = from
		}
		if qp.Error != nil {
			a.errors++
		}
	}
	l.mu.Unlock()
	if t != nil {
		// only the request's own goroutine writes its track
		t.queries++
		t.queryMS += ms
		hit := t.shapes[shape]
		if hit == nil && len(t.shapes) < 200 {
			hit = &shapeHit{caller: from}
			t.shapes[shape] = hit
		}
		if hit != nil {
			hit.times++
		}
	}
	if slow || qp.Error != nil {
		e := SlowQuery{At: now, SQL: clipText(qp.SQL, 2000), Shape: shape, MS: ms, Rows: qp.RowsAffected, Caller: from}
		if qp.Error != nil && !notFound(qp.Error) {
			e.Error = clipText(qp.Error.Error(), 300)
			l.failed.add(e)
		}
		if slow {
			l.slow.add(e)
		}
	}
}

// notFound is the error a lookup gives when there is no such row: an
// answer, not a failure.
func notFound(err error) bool { return err != nil && strings.Contains(err.Error(), "record not found") }

func (l *queryLog) report() *QueryReport {
	l.mu.Lock()
	rep := &QueryReport{Total: l.total, Driver: l.driver, Top: make([]QueryStat, 0, len(l.stats)), SlowMS: float64(l.slowAt.Milliseconds())}
	for shape, a := range l.stats {
		s := QueryStat{Shape: shape, Count: a.count, Errors: a.errors, MaxMS: a.max, TotalMS: a.total, Rows: a.rows, Caller: a.caller, LastAt: a.last}
		if a.count > 0 {
			s.AvgMS = a.total / float64(a.count)
		}
		rep.Top = append(rep.Top, s)
	}
	l.mu.Unlock()
	sort.Slice(rep.Top, func(i, j int) bool { return rep.Top[i].TotalMS > rep.Top[j].TotalMS })
	if len(rep.Top) > 50 {
		rep.Top = rep.Top[:50]
	}
	rep.Slow, rep.Failed = l.slow.newest(60), l.failed.newest(30)
	if rep.Slow == nil {
		rep.Slow = []SlowQuery{}
	}
	if rep.Failed == nil {
		rep.Failed = []SlowQuery{}
	}
	rep.Pool = poolStat()
	return rep
}

func poolStat() (out *PoolStat) {
	defer func() { _ = recover() }() // no database connected
	db := database.Get()
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil || sqlDB == nil {
		return nil
	}
	s := sqlDB.Stats()
	return &PoolStat{Open: s.OpenConnections, InUse: s.InUse, Idle: s.Idle, MaxOpen: s.MaxOpenConnections, WaitCount: s.WaitCount,
		WaitMS: float64(s.WaitDuration.Microseconds()) / 1000, MaxIdleClose: s.MaxIdleClosed}
}
