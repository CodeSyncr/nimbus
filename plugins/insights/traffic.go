package insights

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
Who visits.

A visit is a page a person asked for: a GET answered with a page (HTML, or
the JSON an Inertia link asks for), not a file, an API call, a probe or a
crawler. Each day keeps its views, its visitors, and what they looked at,
came from and came with.

Nobody is followed. There is no cookie. A visitor is told apart for one
day by a number made from their address, their browser and that day's
secret; the number says nothing of either, and the next day's is another.

Days are written to a folder (storage/insights) so they outlive a restart
and a new version of the app.
*/

// Day is one day's visits (UTC).
type Day struct {
	Date      string         `json:"date"` // 2026-10-09
	Views     int            `json:"views"`
	Visitors  int            `json:"visitors"`
	Hours     [24]int        `json:"hours"`
	Paths     map[string]int `json:"paths"`
	Referrers map[string]int `json:"referrers"`
	Countries map[string]int `json:"countries"`
	Devices   map[string]int `json:"devices"`
	Browsers  map[string]int `json:"browsers"`
	// Seen is the day's visitors, as the numbers they are told apart by.
	Seen []uint64 `json:"seen,omitempty"`

	seen  map[uint64]struct{}
	dirty bool
}

// Count is a name and how often.
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DayCount is a day's totals.
type DayCount struct {
	Date     string `json:"date"`
	Views    int    `json:"views"`
	Visitors int    `json:"visitors"`
}

// TrafficReport is the visits of the last days.
type TrafficReport struct {
	Days      []DayCount `json:"days"` // oldest first, a day with no visit included
	Views     int        `json:"views"`
	Visitors  int        `json:"visitors"` // the days' visitors added up
	Today     DayCount   `json:"today"`
	Hours     [24]int    `json:"hours"` // today, by hour (UTC)
	Paths     []Count    `json:"paths"`
	Referrers []Count    `json:"referrers"`
	Countries []Count    `json:"countries"`
	Devices   []Count    `json:"devices"`
	Browsers  []Count    `json:"browsers"`
	Kept      bool       `json:"kept"` // written to disk, so it outlives a restart
}

type trafficLog struct {
	mu     sync.Mutex
	dir    string
	secret string
	days   map[string]*Day
}

func newTrafficLog(dir, secret string) *trafficLog {
	if dir == "-" {
		dir = ""
	}
	return &trafficLog{dir: dir, secret: secret, days: map[string]*Day{}}
}

func newDay(date string) *Day {
	return &Day{Date: date, Paths: map[string]int{}, Referrers: map[string]int{}, Countries: map[string]int{}, Devices: map[string]int{}, Browsers: map[string]int{}, seen: map[uint64]struct{}{}}
}

// day is a day's record: in memory, else read from its file, else new.
func (l *trafficLog) day(date string, create bool) *Day {
	if d := l.days[date]; d != nil {
		return d
	}
	if l.dir != "" {
		if data, err := os.ReadFile(filepath.Join(l.dir, "traffic-"+date+".json")); err == nil {
			d := newDay(date)
			if json.Unmarshal(data, d) == nil && d.Date == date {
				for _, h := range d.Seen {
					d.seen[h] = struct{}{}
				}
				d.Seen = nil
				for _, m := range []*map[string]int{&d.Paths, &d.Referrers, &d.Countries, &d.Devices, &d.Browsers} {
					if *m == nil {
						*m = map[string]int{}
					}
				}
				l.days[date] = d
				return d
			}
		}
	}
	if !create {
		return nil
	}
	d := newDay(date)
	l.days[date] = d
	return d
}

var botWords = []string{"bot", "crawl", "spider", "slurp", "preview", "monitor", "uptime", "curl/", "wget/", "python-requests", "go-http-client", "headless", "lighthouse", "pingdom", "facebookexternalhit", "scrapy", "httpclient", "okhttp", "axios/", "node-fetch"}

func isBot(ua string) bool {
	if ua == "" {
		return true
	}
	low := strings.ToLower(ua)
	for _, w := range botWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

func device(ua string) string {
	low := strings.ToLower(ua)
	switch {
	case strings.Contains(low, "ipad") || strings.Contains(low, "tablet") || (strings.Contains(low, "android") && !strings.Contains(low, "mobile")):
		return "tablet"
	case strings.Contains(low, "mobi") || strings.Contains(low, "iphone") || strings.Contains(low, "android"):
		return "mobile"
	}
	return "desktop"
}

func browser(ua string) string {
	low := strings.ToLower(ua)
	switch {
	case strings.Contains(low, "edg/"):
		return "Edge"
	case strings.Contains(low, "opr/") || strings.Contains(low, "opera"):
		return "Opera"
	case strings.Contains(low, "firefox/"):
		return "Firefox"
	case strings.Contains(low, "chrome/") || strings.Contains(low, "crios/"):
		return "Chrome"
	case strings.Contains(low, "safari/"):
		return "Safari"
	}
	return "Other"
}

// clientIP is the visitor's address as the proxies in front report it.
func clientIP(r *http.Request) string {
	for _, h := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func bump(m map[string]int, key string, max int) {
	if key == "" {
		return
	}
	if _, ok := m[key]; !ok && len(m) >= max {
		key = "(other)"
	}
	m[key]++
}

// visit counts a request when it is a person looking at a page.
func (l *trafficLog) visit(r *http.Request, status int, contentType string) {
	if l == nil || r.Method != http.MethodGet || status >= 400 || status == http.StatusNotModified {
		return
	}
	inertia := r.Header.Get("X-Inertia") != ""
	if !inertia && !strings.HasPrefix(contentType, "text/html") {
		return
	}
	if r.Header.Get("X-Inertia-Partial-Data") != "" { // a part of a page already counted
		return
	}
	ua := r.UserAgent()
	if isBot(ua) || r.Header.Get("Purpose") == "prefetch" || r.Header.Get("Sec-Purpose") != "" {
		return
	}
	now := time.Now().UTC()
	date := now.Format("2006-01-02")
	sum := sha256.Sum256([]byte(l.secret + "|" + date + "|" + clientIP(r) + "|" + ua))
	who := binary.BigEndian.Uint64(sum[:8])
	ref := ""
	if raw := r.Referer(); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Host != "" && !strings.EqualFold(u.Host, r.Host) {
			ref = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		}
	}
	path := r.URL.Path
	if len(path) > 120 {
		path = path[:120]
	}
	country := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	if country == "XX" || country == "T1" {
		country = ""
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.day(date, true)
	d.Views++
	d.Hours[now.Hour()]++
	bump(d.Paths, path, 300)
	if _, known := d.seen[who]; !known {
		if len(d.seen) < 200000 {
			d.seen[who] = struct{}{}
		}
		d.Visitors++
		// what a visitor came from and with is counted once a day, not on every page
		if ref == "" {
			ref = "(direct)"
		}
		bump(d.Referrers, ref, 200)
		bump(d.Countries, country, 260)
		bump(d.Devices, device(ua), 10)
		bump(d.Browsers, browser(ua), 20)
	}
	d.dirty = true
}

// flush writes the days that changed, and forgets and removes old ones.
func (l *trafficLog) flush() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	for date, d := range l.days {
		if d.dirty && l.dir != "" {
			if err := os.MkdirAll(l.dir, 0o755); err == nil {
				d.Seen = d.Seen[:0]
				for h := range d.seen {
					d.Seen = append(d.Seen, h)
				}
				sort.Slice(d.Seen, func(i, j int) bool { return d.Seen[i] < d.Seen[j] })
				if data, err := json.Marshal(d); err == nil {
					tmp := filepath.Join(l.dir, "traffic-"+date+".json.tmp")
					if os.WriteFile(tmp, data, 0o644) == nil {
						_ = os.Rename(tmp, filepath.Join(l.dir, "traffic-"+date+".json"))
					}
				}
				d.Seen = nil
			}
			d.dirty = false
		}
		if date != today && !d.dirty {
			delete(l.days, date) // read again from its file when asked for
		}
	}
	if l.dir == "" {
		return
	}
	// a hundred days are kept
	cutoff := time.Now().UTC().AddDate(0, 0, -100).Format("2006-01-02")
	if names, err := filepath.Glob(filepath.Join(l.dir, "traffic-*.json")); err == nil {
		for _, n := range names {
			if date := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(n), "traffic-"), ".json"); date < cutoff {
				_ = os.Remove(n)
			}
		}
	}
}

func top(m map[string]int, n int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (l *trafficLog) report(days int) *TrafficReport {
	if l == nil {
		return nil
	}
	if days < 1 {
		days = 30
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	rep := &TrafficReport{Kept: l.dir != ""}
	paths, refs, countries, devices, browsers := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		dc := DayCount{Date: date}
		_, held := l.days[date]
		if d := l.day(date, false); d != nil {
			dc.Views, dc.Visitors = d.Views, d.Visitors
			for k, v := range d.Paths {
				paths[k] += v
			}
			for k, v := range d.Referrers {
				refs[k] += v
			}
			for k, v := range d.Countries {
				countries[k] += v
			}
			for k, v := range d.Devices {
				devices[k] += v
			}
			for k, v := range d.Browsers {
				browsers[k] += v
			}
			if date == today {
				rep.Hours = d.Hours
			} else if !held {
				delete(l.days, date) // read for this answer only
			}
		}
		rep.Days = append(rep.Days, dc)
		rep.Views += dc.Views
		rep.Visitors += dc.Visitors
		if date == today {
			rep.Today = dc
		}
	}
	rep.Paths, rep.Referrers, rep.Countries, rep.Devices, rep.Browsers = top(paths, 25), top(refs, 20), top(countries, 20), top(devices, 5), top(browsers, 8)
	return rep
}
