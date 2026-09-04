package startupview

import (
	"strings"
	"testing"
	"time"
)

// A DSN can carry credentials; the startup view must never print them.
func TestDSNIsDescribedWithoutCredentials(t *testing.T) {
	cases := map[string]string{
		"postgresql://postgres:hunter2@db.supabase.co:5432/postgres": "db.supabase.co:5432/postgres",
		"mysql://root:secret@127.0.0.1:3306/shop":                    "127.0.0.1:3306/shop",
		"database/nimbus.sqlite":                                     "database/nimbus.sqlite",
		"":                                                           "",
	}
	for dsn, want := range cases {
		got := DescribeDSN(dsn)
		if got != want {
			t.Errorf("DescribeDSN(%q) = %q, want %q", dsn, got, want)
		}
		if strings.Contains(got, "hunter2") || strings.Contains(got, "secret") {
			t.Errorf("credentials leaked into the startup view: %q", got)
		}
	}

	// A query string can carry a password too.
	if got := DescribeDSN("postgres://u:p@host:5432/db?sslmode=require&password=x"); strings.Contains(got, "password=") {
		t.Errorf("query string was not dropped: %q", got)
	}
}

func TestBootTimeReadsNaturally(t *testing.T) {
	cases := map[time.Duration]string{
		400 * time.Microsecond:  "0.4ms",
		14 * time.Millisecond:   "14ms",
		2500 * time.Millisecond: "2.50s",
	}
	for d, want := range cases {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

// Piping the output somewhere must not fill the file with escape codes. In
// tests stdout is not a terminal, so colour is already off.
func TestColourIsSkippedWhenNotATerminal(t *testing.T) {
	if colorEnabled {
		t.Skip("stdout is a terminal in this environment")
	}
	if got := paint(ansiGreen, "ok"); got != "ok" {
		t.Errorf("paint added escapes with colour disabled: %q", got)
	}
}

// The marker is how the app hands its boot report to the CLI. Anything lost in
// transit is a fact missing from the terminal.
func TestMarkerSurvivesTheRoundTrip(t *testing.T) {
	want := Info{
		Name: "nimbus", Env: "development", Version: "v1.5.4",
		Go: "1.26", OS: "darwin", Arch: "arm64",
		Scheme: "http", Port: "3333", LAN: "192.168.1.104",
		DBDriver: "sqlite", DBSource: "database/nimbus.sqlite",
		Providers: 12, Plugins: 3,
		Routes: 28, RouteCompile: 340 * time.Microsecond,
		ViewExt: ".nimbus", ViewLiveReload: true,
		Booted: 14 * time.Millisecond, Watching: true,
	}

	line := want.Marker()
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("marker must be a single line, got %q", line)
	}
	if !IsMarker(line) {
		t.Fatalf("IsMarker did not recognise its own marker: %q", line)
	}

	got, ok := ParseMarker(line)
	if !ok {
		t.Fatalf("ParseMarker rejected its own marker: %q", line)
	}
	if got != want {
		t.Errorf("round trip changed the report:\n got %+v\nwant %+v", got, want)
	}
}

// A new CLI can front an app built against an older nimbus. It must still show
// something rather than leaking the raw marker into the terminal.
func TestLegacyPipeMarkerStillParses(t *testing.T) {
	got, ok := ParseMarker("__NIMBUS_READY__|https|8080|shop|production|4")
	if !ok {
		t.Fatal("legacy marker was rejected")
	}
	if got.Scheme != "https" || got.Port != "8080" || got.Name != "shop" ||
		got.Env != "production" || got.Plugins != 4 {
		t.Errorf("legacy marker parsed wrong: %+v", got)
	}
}

func TestNonMarkerLinesArePassedOver(t *testing.T) {
	for _, line := range []string{"", "[HTTP] GET / 200 OK", "__NIMBUS_READY__ {broken"} {
		if _, ok := ParseMarker(line); ok {
			t.Errorf("ParseMarker accepted %q", line)
		}
	}
}

// Every fact in the report has to reach the rendered text.
func TestRenderShowsEveryFact(t *testing.T) {
	out := Render(Info{
		Name: "nimbus", Env: "development", Version: "v1.5.4",
		Go: "1.26", OS: "darwin", Arch: "arm64",
		Scheme: "http", Port: "3333", LAN: "192.168.1.104",
		DBDriver: "sqlite", DBSource: "database/nimbus.sqlite",
		Providers: 12, Plugins: 3,
		Routes: 28, RouteCompile: 340 * time.Microsecond,
		ViewExt: ".nimbus", ViewLiveReload: true,
		Booted: 14 * time.Millisecond, Watching: true,
	})

	for _, want := range []string{
		"[NIMBUS]", "v1.5.4", "(Go 1.26 darwin/arm64)",
		"Environment:", "development",
		"Database:", "sqlite", "(database/nimbus.sqlite)",
		"Service Providers:", "12 registered", "(3 plugins)",
		"Router:", "28 routes", "compiled in 0.34ms",
		"View Engine:", ".nimbus", "(live-reload active)",
		"Local:", "http://localhost:3333",
		"Network:", "http://192.168.1.104:3333",
		"Ready in 14ms", "(watching for changes…)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("startup view is missing %q:\n%s", want, out)
		}
	}
}

// An app with no database or views should not print empty facts for them.
func TestRenderOmitsFactsThatDoNotApply(t *testing.T) {
	out := Render(Info{Version: "v1.5.4", Port: "3000", Routes: 1})
	if strings.Contains(out, "Database:") {
		t.Error("printed a Database fact for an app without one")
	}
	if strings.Contains(out, "View Engine:") {
		t.Error("printed a View Engine fact for an app without one")
	}
	if strings.Contains(out, "Network:") {
		t.Error("printed a Network line without a LAN address")
	}
}

// `nimbus serve` compiles, builds assets and migrates before the app can
// report itself. That stretch used to be silent, which reads as a hang.
func TestLaunchBannerSaysSomethingImmediately(t *testing.T) {
	out := RenderLaunch(Launch{})
	if !strings.Contains(out, "[NIMBUS]") {
		t.Error("the launch banner does not identify itself")
	}
	if !strings.Contains(out, "Watching:") {
		t.Errorf("the launch banner does not say what triggers a reload:\n%s", out)
	}
	// The default list is shown when the caller does not name one.
	if !strings.Contains(out, ".nimbus") {
		t.Errorf("no watched extensions listed:\n%s", out)
	}

	// The wordmark belongs to the boot report, which redraws on every hot
	// reload; repeating it here would print it twice per rebuild.
	if strings.Contains(out, "|_| \\_|_|_|") {
		t.Errorf("the launch banner duplicates the logo:\n%s", out)
	}
	if !strings.Contains(Render(Info{Port: "3000"}), "|_| \\_|_|_|") {
		t.Error("the boot report lost the logo")
	}
}

// An Inertia app runs a second dev server, and its URL is worth knowing before
// anything has compiled.
func TestLaunchBannerMentionsInertiaOnlyWhenPresent(t *testing.T) {
	if got := RenderLaunch(Launch{}); strings.Contains(got, "inertia") {
		t.Errorf("a plain app was told about inertia:\n%s", got)
	}
	got := RenderLaunch(Launch{Inertia: true})
	if !strings.Contains(got, "inertia/") || !strings.Contains(got, "5173") {
		t.Errorf("an inertia app was not told where the frontend runs:\n%s", got)
	}
}

// A caller that names what it watches gets that, not the default.
func TestLaunchBannerHonoursAStatedWatchList(t *testing.T) {
	got := RenderLaunch(Launch{Watching: ".go only"})
	if !strings.Contains(got, ".go only") {
		t.Errorf("the stated watch list was ignored:\n%s", got)
	}
	if strings.Contains(got, ".css") {
		t.Errorf("the default list leaked through:\n%s", got)
	}
}
