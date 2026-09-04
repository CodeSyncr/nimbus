package nimbus

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/internal/startupview"
	"github.com/CodeSyncr/nimbus/internal/version"
	"github.com/CodeSyncr/nimbus/view"
)

/*
The startup view.

What a framework prints on boot is the first thing anyone sees of it, and it is
read far more often than it is written. This gathers what the boot report needs
to answer the questions actually asked when a server starts: which build is
this, what is it connected to, did everything register, how many routes are
live, and where do I open it.

Drawing it is startupview's job, because the same report is drawn by the CLI
when the app runs under `nimbus serve` and cannot reach the terminal itself.
*/

// startupInfo collects the boot report for this app.
func (a *App) startupInfo(scheme, port string, booted time.Duration) startupview.Info {
	name := a.Config.App.Name
	if name == "" {
		name = "nimbus"
	}
	env := a.Config.App.Env
	if env == "" {
		env = "development"
	}

	info := startupview.Info{
		Name:      name,
		Env:       env,
		Version:   version.Nimbus,
		Go:        goVersion(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Scheme:    scheme,
		Port:      port,
		LAN:       startupview.LANAddress(),
		DBDriver:  a.Config.Database.Driver,
		DBSource:  startupview.DescribeDSN(a.Config.Database.DSN),
		Providers: len(a.providers),
		Plugins:   len(a.plugins),
		Booted:    booted,
		Watching:  env != "production",
	}

	if a.Router != nil {
		info.Routes = len(a.Router.Routes())
		info.RouteCompile = a.Router.CompileTime()
	}

	// Views are optional: report the engine only when the app actually has a
	// template directory. Live reload is Air's, so it is only real under
	// `nimbus serve`.
	if root := view.DefaultRoot(); root != "" {
		if st, err := os.Stat(root); err == nil && st.IsDir() {
			info.ViewExt = ".nimbus"
			info.ViewLiveReload = env != "production" && os.Getenv("NIMBUS_SERVE") == "1"
		}
	}

	return info
}

// goVersion strips the "go" prefix from the runtime version.
func goVersion() string {
	return strings.TrimPrefix(runtime.Version(), "go")
}
