// Package startupview renders the report a Nimbus app prints when it boots.
//
// It lives apart from the App because two different processes need to draw it.
// A direct `go run .` renders the view itself. Under `nimbus serve` the app is
// a child of Air with a pipe for stdout — it can neither colour its output nor
// know when the CLI is ready for it — so it emits the same facts as a
// single-line marker and the CLI, which does own a terminal, renders them.
// One renderer, one look, however the app was started.
package startupview

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// markerPrefix opens the line an app emits under `nimbus serve` instead of
// drawing the view itself. The CLI watches for it to know the app is up.
const markerPrefix = "__NIMBUS_READY__"

// ANSI codes, used directly to keep boot dependency-free.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
)

// colorEnabled reports whether to emit escapes. A redirected stdout or
// NO_COLOR means plain text.
var colorEnabled = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ColorEnabled reports whether this process will colour the view.
func ColorEnabled() bool { return colorEnabled }

func paint(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

// logo is the wordmark shown on boot.
const logo = `    _   _ _           _
   | \ | (_)_ __ ___ | |__  _   _ ___
   |  \| | | '_ ' _ \| '_ \| | | / __|
   | |\  | | | | | | | |_) | |_| \__ \
   |_| \_|_|_| |_| |_|_.__/ \__,_|___/`

// Info is everything the boot report shows. It crosses a process boundary as
// JSON, so every field is exported and tagged.
type Info struct {
	Name    string `json:"name"`
	Env     string `json:"env"`
	Version string `json:"version"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`

	Scheme string `json:"scheme"`
	Port   string `json:"port"`
	LAN    string `json:"lan,omitempty"`

	DBDriver string `json:"db_driver,omitempty"`
	DBSource string `json:"db_source,omitempty"`

	Providers int `json:"providers"`
	Plugins   int `json:"plugins"`

	Routes       int           `json:"routes"`
	RouteCompile time.Duration `json:"route_compile"`

	ViewExt        string `json:"view_ext,omitempty"`
	ViewLiveReload bool   `json:"view_live_reload,omitempty"`

	Booted   time.Duration `json:"booted"`
	Watching bool          `json:"watching,omitempty"`
}

// Marker encodes the view for a process that cannot draw it.
func (i Info) Marker() string {
	b, err := json.Marshal(i)
	if err != nil {
		// Never fail a boot over the banner.
		return markerPrefix + " {}"
	}
	return markerPrefix + " " + string(b)
}

// IsMarker reports whether a line of app output carries a boot report.
func IsMarker(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), markerPrefix)
}

// ParseMarker decodes a marker line. It also understands the pipe-delimited
// form emitted by apps built against nimbus ≤ v1.5.4, so a new CLI in front of
// an older app still shows something sensible rather than a raw marker.
func ParseMarker(line string) (Info, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, markerPrefix) {
		return Info{}, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, markerPrefix))

	if strings.HasPrefix(rest, "{") {
		var info Info
		if err := json.Unmarshal([]byte(rest), &info); err != nil {
			return Info{}, false
		}
		return info, true
	}

	// Legacy: __NIMBUS_READY__|scheme|port|name|env|plugins
	parts := strings.Split(line, "|")
	if len(parts) < 6 {
		return Info{}, false
	}
	info := Info{Scheme: parts[1], Port: parts[2], Name: parts[3], Env: parts[4], Watching: true}
	fmt.Sscanf(parts[5], "%d", &info.Plugins)
	return info, true
}

// Launch is what `nimbus serve` knows before the app exists: the command has
// just been typed and nothing has compiled yet.
type Launch struct {
	// Inertia reports a frontend dev server running alongside.
	Inertia bool
	// Watching lists what triggers a rebuild.
	Watching string
}

// RenderLaunch draws the line shown the moment `nimbus serve` starts.
//
// It exists because the boot report cannot: that one needs a running app, and
// between the command being typed and the app being up there is a compile, an
// asset build and — in a real project — schema migrations. That gap was
// silent, which reads as a hang rather than as work in progress. This says
// what is happening immediately, and the boot report replaces it when the app
// is actually serving.
func RenderLaunch(l Launch) string {
	watching := l.Watching
	if watching == "" {
		watching = ".go, .nimbus, .css, .js"
	}

	// No logo here on purpose: the boot report carries it, and that report is
	// redrawn on every hot reload. Showing the wordmark twice within a few
	// seconds — and again on each rebuild — is noise, so this stays compact.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s %s\n",
		paint(ansiBold+ansiBlue, "[NIMBUS]"),
		paint(ansiDim, "dev server · hot reload"),
	))
	b.WriteString(fmt.Sprintf("  %s %s\n",
		paint(ansiDim, "Watching:"), paint(ansiCyan, watching)))
	if l.Inertia {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			paint(ansiDim, "Frontend:"),
			paint(ansiCyan, "inertia/")+" "+paint(ansiDim, "(HMR on :5173)")))
	}
	b.WriteString("\n")
	return b.String()
}

// fact is one line of the boot report.
type fact struct {
	label string
	value string
	ok    bool
}

// Render draws the full boot report.
func Render(i Info) string {
	if i.Env == "" {
		i.Env = "development"
	}
	if i.Scheme == "" {
		i.Scheme = "http"
	}

	var b strings.Builder
	b.WriteString("\n" + paint(ansiCyan, logo) + "\n\n")

	// Build line: which nimbus, which Go, which machine.
	build := ""
	if i.Go != "" {
		build = fmt.Sprintf("(Go %s %s/%s)", i.Go, i.OS, i.Arch)
	}
	b.WriteString(fmt.Sprintf("  %s %s %s\n",
		paint(ansiBold+ansiBlue, "[NIMBUS]"),
		paint(ansiBold, i.Version),
		paint(ansiDim, build),
	))

	for _, f := range facts(i) {
		mark := paint(ansiGreen, "✓")
		if !f.ok {
			mark = paint(ansiYellow, "!")
		}
		b.WriteString(fmt.Sprintf("  %s %s %s\n", mark, paint(ansiDim, f.label+":"), f.value))
	}

	b.WriteString("\n")
	local := fmt.Sprintf("%s://localhost:%s", i.Scheme, i.Port)
	b.WriteString(fmt.Sprintf("  %s %s   %s\n",
		paint(ansiGreen, "→"), paint(ansiBold, "Local:"), paint(ansiBold+ansiCyan, local)))
	if i.LAN != "" {
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			paint(ansiGreen, "→"), paint(ansiDim, "Network:"),
			paint(ansiDim, fmt.Sprintf("%s://%s:%s", i.Scheme, i.LAN, i.Port))))
	}

	ready := fmt.Sprintf("Ready in %s", FormatDuration(i.Booted))
	if i.Watching {
		ready += " (watching for changes…)"
	}
	b.WriteString(fmt.Sprintf("  %s %s\n\n", paint(ansiDim, "→"), paint(ansiDim, ready)))

	return b.String()
}

// facts gathers what is worth reporting, in the order it is read.
func facts(i Info) []fact {
	list := []fact{{label: "Environment", value: paintEnv(i.Env), ok: true}}

	if i.DBDriver != "" {
		value := paint(ansiCyan, i.DBDriver)
		if i.DBSource != "" {
			value += " " + paint(ansiDim, "("+i.DBSource+")")
		}
		list = append(list, fact{label: "Database", value: value, ok: true})
	}

	list = append(list, fact{
		label: "Service Providers",
		value: fmt.Sprintf("%d registered %s", i.Providers, paint(ansiDim, fmt.Sprintf("(%d plugins)", i.Plugins))),
		ok:    true,
	})

	routes := fmt.Sprintf("%d routes", i.Routes)
	if i.RouteCompile > 0 {
		routes += " " + paint(ansiDim, "compiled in "+formatPrecise(i.RouteCompile))
	}
	list = append(list, fact{label: "Router", value: routes, ok: i.Routes > 0})

	if i.ViewExt != "" {
		value := paint(ansiCyan, i.ViewExt)
		if i.ViewLiveReload {
			value += " " + paint(ansiDim, "(live-reload active)")
		}
		list = append(list, fact{label: "View Engine", value: value, ok: true})
	}

	return list
}

// paintEnv colours the environment by how much care it deserves.
func paintEnv(env string) string {
	switch strings.ToLower(env) {
	case "production":
		return paint(ansiRed+ansiBold, env)
	case "staging":
		return paint(ansiYellow, env)
	default:
		return paint(ansiYellow, env)
	}
}

// DescribeDSN reduces a connection string to something safe to print: a file
// path stays as it is, a URL keeps only host and database, and credentials
// never appear.
func DescribeDSN(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	if !strings.Contains(dsn, "://") {
		if len(dsn) > 48 {
			return dsn[:45] + "…"
		}
		return dsn
	}

	rest := dsn[strings.Index(dsn, "://")+3:]
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:] // drop user:password
	}
	if q := strings.IndexAny(rest, "?"); q >= 0 {
		rest = rest[:q]
	}
	if len(rest) > 48 {
		rest = rest[:45] + "…"
	}
	return rest
}

// LANAddress finds the machine's address on the local network, so the URL can
// be opened from a phone or another machine.
func LANAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		if ipNet.IP.IsPrivate() {
			return ipNet.IP.String()
		}
	}
	return ""
}

// formatPrecise renders a duration that is expected to be sub-millisecond,
// where FormatDuration's single decimal would round away what is being read.
func formatPrecise(d time.Duration) string {
	if d >= time.Millisecond {
		return FormatDuration(d)
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
}

// FormatDuration renders a boot duration the way it is talked about.
func FormatDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
}
