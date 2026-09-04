package ai

import (
	"fmt"
	"strings"
)

/*
Command policy for the agent's bash tool.

The previous guard was a substring blocklist over the raw command string
("curl ", "rm -rf /", …). Substring matching is the wrong shape for this job:
`curl\thttp://x` slips through on whitespace, `bash -c 'curl …'` hides the
token one level down, and a legitimate command mentioning a blocked word in a
path or a commit message is refused for no reason.

This classifies the command instead. It splits on shell separators, looks at
the binary each segment actually invokes (unwrapping env/sudo/nohup and
`sh -c` style wrappers), and decides:

	Allowed   run it
	Ask       run it only with the user's consent for this command
	Blocked   refuse

Nothing here is a security boundary — a determined model can obfuscate a shell
command, and the shell is inherently expressive. It is a guardrail against the
realistic failure: a model that fetches from the network or destroys work
outside the project because nothing stopped it.
*/

// CommandRisk is the outcome of classifying a command.
type CommandRisk int

const (
	// RiskAllowed is an ordinary workspace command.
	RiskAllowed CommandRisk = iota
	// RiskAsk needs the user's approval before running.
	RiskAsk
	// RiskBlocked is never run.
	RiskBlocked
)

// CommandVerdict explains a classification.
type CommandVerdict struct {
	Risk    CommandRisk
	Command string // the offending segment
	Reason  string // human-readable, shown in the approval prompt
}

// networkBinaries reach outside the machine.
//
// Fetching is allowed: users pass reference links ("make it look like
// https://stripe.com"), point the agent at their own deployment, and expect it
// to read documentation. Blocking that outright made the agent useless for
// ordinary work.
//
// What is guarded is the shape that actually hurts — sending local data out,
// and running code that came back down the wire. See classifyNetwork.
var networkBinaries = map[string]bool{
	"curl": true, "wget": true, "http": true, "httpie": true, "aria2c": true,
	"nc": true, "ncat": true, "netcat": true, "telnet": true, "socat": true,
	"ssh": true, "scp": true, "sftp": true, "rsync": true, "ftp": true, "tftp": true,
	"kubectl": true,
}

// uploadFlags mark a request carrying local file content outward.
var uploadFlags = []string{
	"--upload-file", "--data-binary @", "--data @", "-d @", "-F @", "--form @",
	"-T ", "--data-urlencode @", "--data-raw @",
}

// shellInterpreters are what a downloaded payload must never be piped into.
var shellInterpreters = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true,
	"python": true, "python3": true, "perl": true, "ruby": true, "node": true,
	"powershell": true, "pwsh": true,
}

// destructiveBinaries wipe or reformat storage. There is no workspace task
// that needs them.
var destructiveBinaries = map[string]string{
	"mkfs":           "formats a filesystem",
	"mkfs.ext4":      "formats a filesystem",
	"fdisk":          "edits disk partitions",
	"parted":         "edits disk partitions",
	"shutdown":       "halts the machine",
	"reboot":         "restarts the machine",
	"halt":           "halts the machine",
	"userdel":        "deletes a system account",
	"passwd":         "changes credentials",
	"visudo":         "edits sudoers",
	"diskutil":       "manages disks",
	"softwareupdate": "changes system software",
}

// askBinaries are legitimate but consequential: fine when the user meant them,
// bad as a silent side effect of "fix the tests".
var askBinaries = map[string]string{
	"sudo":           "runs a command as root",
	"doas":           "runs a command as root",
	"chown":          "changes file ownership",
	"chmod":          "changes file permissions",
	"kill":           "terminates a process",
	"killall":        "terminates processes by name",
	"pkill":          "terminates processes by name",
	"docker":         "controls containers on this machine",
	"docker-compose": "controls containers on this machine",
	"systemctl":      "controls system services",
	"launchctl":      "controls system services",
	"brew":           "installs or removes software",
	"apt":            "installs or removes software",
	"apt-get":        "installs or removes software",
	"yum":            "installs or removes software",
	"apk":            "installs or removes software",
	"pip":            "installs packages",
	"pip3":           "installs packages",
	"npm":            "can run arbitrary lifecycle scripts",
	"pnpm":           "can run arbitrary lifecycle scripts",
	"yarn":           "can run arbitrary lifecycle scripts",
}

// wrapperBinaries pass the real command through as arguments, so the decision
// belongs to what they wrap.
var wrapperBinaries = map[string]bool{
	"env": true, "nohup": true, "time": true, "nice": true, "xargs": true,
	"timeout": true, "stdbuf": true, "command": true, "exec": true,
}

// shellBinaries take a command string via -c; the payload is inspected.
var shellBinaries = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true,
	"fish": true, "powershell": true, "pwsh": true, "cmd": true,
}

// ClassifyCommand decides whether a command may run unattended.
func ClassifyCommand(command string) CommandVerdict {
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandVerdict{Risk: RiskBlocked, Reason: "empty command"}
	}

	// Checked before splitting: a fork bomb is made of the same separators
	// the splitter breaks on, so its fragments look harmless on their own.
	if isForkBomb(command) {
		return CommandVerdict{Risk: RiskBlocked, Command: command, Reason: "fork bomb"}
	}
	// A shell wrapper hides its payload in a quoted argument; splitting the
	// outer command on separators would tear that payload apart before it can
	// be judged, so unwrap it first.
	if fields := strings.Fields(command); len(fields) > 1 && shellBinaries[binaryName(fields[0])] {
		if inner := shellPayload(fields[1:]); inner != "" {
			v := ClassifyCommand(inner)
			if v.Risk != RiskAllowed {
				if v.Command == "" {
					v.Command = command
				}
				return v
			}
		}
	}
	if pipesRemoteCodeIntoShell(command) {
		return CommandVerdict{
			Risk:    RiskBlocked,
			Command: command,
			Reason:  "runs code downloaded from the network",
		}
	}

	worst := CommandVerdict{Risk: RiskAllowed}
	for _, segment := range splitSegments(command) {
		v := classifySegment(segment)
		if v.Risk > worst.Risk {
			worst = v
		}
		if worst.Risk == RiskBlocked {
			break
		}
	}
	return worst
}

// classifySegment inspects one simple command.
func classifySegment(segment string) CommandVerdict {
	fields := strings.Fields(segment)
	if len(fields) == 0 {
		return CommandVerdict{Risk: RiskAllowed}
	}

	bin, args := binaryName(fields[0]), fields[1:]

	// Unwrap `env FOO=bar cmd`, `nohup cmd`, `timeout 5 cmd`, …
	for wrapperBinaries[bin] && len(args) > 0 {
		next := 0
		for next < len(args) && (strings.Contains(args[next], "=") || strings.HasPrefix(args[next], "-") || isNumeric(args[next])) {
			next++
		}
		if next >= len(args) {
			return CommandVerdict{Risk: RiskAllowed}
		}
		bin, args = binaryName(args[next]), args[next+1:]
	}

	// `sh -c "…"` hides the real command in a quoted argument: classify that.
	if shellBinaries[bin] {
		if inner := shellPayload(args); inner != "" {
			v := ClassifyCommand(inner)
			if v.Risk != RiskAllowed && v.Command == "" {
				v.Command = segment
			}
			return v
		}
	}

	if reason, ok := destructiveBinaries[bin]; ok {
		return CommandVerdict{Risk: RiskBlocked, Command: segment, Reason: reason}
	}
	if networkBinaries[bin] {
		return classifyNetwork(bin, args, segment)
	}
	if v, hit := classifyRemoval(bin, args, segment); hit {
		return v
	}
	if v, hit := classifyGit(bin, args, segment); hit {
		return v
	}
	if reason, ok := askBinaries[bin]; ok {
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: reason}
	}
	return CommandVerdict{Risk: RiskAllowed}
}

// classifyNetwork judges a command that talks to the network.
//
//	fetching a URL          allowed — this is how references and your own
//	                        deployment get looked at
//	uploading local data    ask — the exfiltration shape
//	remote shell / copy     ask — ssh, scp, rsync to a remote host
//
// Piping a download into an interpreter is handled before this, in
// ClassifyCommand, because the danger lives in the pipeline rather than in
// either command alone.
func classifyNetwork(bin string, args []string, segment string) CommandVerdict {
	joined := " " + strings.Join(args, " ") + " "

	for _, flag := range uploadFlags {
		if strings.Contains(joined, " "+strings.TrimSpace(flag)) || strings.Contains(joined, flag) {
			return CommandVerdict{
				Risk:    RiskAsk,
				Command: segment,
				Reason:  "sends local file contents to " + describeTarget(args),
			}
		}
	}

	switch bin {
	case "ssh", "telnet":
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "opens a shell on " + describeTarget(args)}
	case "scp", "sftp", "rsync", "ftp", "tftp":
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "copies files to or from " + describeTarget(args)}
	case "nc", "ncat", "netcat", "socat":
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "opens a raw network connection"}
	case "kubectl":
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "acts on a remote cluster"}
	}

	// A plain fetch. Reading a URL is ordinary work.
	return CommandVerdict{Risk: RiskAllowed}
}

// describeTarget names the host or URL a command is aimed at, so an approval
// prompt says where the data is going rather than just "the network".
func describeTarget(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if strings.Contains(a, "://") || strings.Contains(a, "@") || strings.Contains(a, ".") {
			return strings.Trim(a, `"'`)
		}
	}
	return "a remote host"
}

// pipesRemoteCodeIntoShell reports the "curl … | sh" shape: the download is
// the program being executed.
//
// The distinction that matters is whether the interpreter has its own program.
// "curl … | python3 -c '…'" runs a script the model wrote and feeds it the
// download as *data* — that is ordinary text processing and must be allowed.
// "curl … | python3" (or "| sh", or "| python3 -") makes the downloaded bytes
// the program itself, which is the shape with no legitimate use here.
func pipesRemoteCodeIntoShell(command string) bool {
	segments := strings.Split(command, "|")
	if len(segments) < 2 {
		return false
	}
	var sawFetch bool
	for i, seg := range segments {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		bin := binaryName(fields[0])
		if (i == 0 || sawFetch) && networkBinaries[bin] {
			sawFetch = true
			continue
		}
		if sawFetch && shellInterpreters[bin] && readsProgramFromStdin(fields[1:]) {
			return true
		}
	}
	return false
}

// readsProgramFromStdin reports whether an interpreter invoked with these
// arguments would take its program from stdin.
func readsProgramFromStdin(args []string) bool {
	for _, a := range args {
		switch a {
		case "-c", "-e", "-E", "--command", "-Command":
			return false // the program is the next argument
		case "-":
			return true // explicitly "read the program from stdin"
		}
		// A script file argument means stdin is data for that script.
		if !strings.HasPrefix(a, "-") {
			return false
		}
	}
	// No program anywhere: stdin is the program.
	return true
}

// classifyRemoval judges `rm`: inside the workspace it is routine, and a
// recursive delete aimed outside it is not.
func classifyRemoval(bin string, args []string, segment string) (CommandVerdict, bool) {
	if bin != "rm" && bin != "rmdir" && bin != "trash" {
		return CommandVerdict{}, false
	}
	recursive := false
	var targets []string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--"):
			if a == "--recursive" || a == "--force" {
				recursive = recursive || a == "--recursive"
			}
		case strings.HasPrefix(a, "-"):
			if strings.ContainsAny(a, "rR") {
				recursive = true
			}
		default:
			targets = append(targets, a)
		}
	}

	for _, target := range targets {
		if isDangerousTarget(target) {
			return CommandVerdict{
				Risk:    RiskBlocked,
				Command: segment,
				Reason:  fmt.Sprintf("deletes %q, outside the project", target),
			}, true
		}
		if recursive && escapesWorkspace(target) {
			return CommandVerdict{
				Risk:    RiskAsk,
				Command: segment,
				Reason:  fmt.Sprintf("recursively deletes %q", target),
			}, true
		}
	}
	return CommandVerdict{}, false
}

// classifyGit flags the git operations that discard or publish work.
func classifyGit(bin string, args []string, segment string) (CommandVerdict, bool) {
	if bin != "git" || len(args) == 0 {
		return CommandVerdict{}, false
	}
	joined := strings.Join(args, " ")
	switch {
	case args[0] == "push" && (strings.Contains(joined, "--force") || strings.Contains(joined, "-f")):
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "force-pushes, which can overwrite remote history"}, true
	case args[0] == "push":
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "publishes commits to a remote"}, true
	case args[0] == "reset" && strings.Contains(joined, "--hard"):
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "discards uncommitted changes"}, true
	case args[0] == "clean" && strings.ContainsAny(joined, "fF"):
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "deletes untracked files"}, true
	case args[0] == "checkout" && strings.Contains(joined, "--force"):
		return CommandVerdict{Risk: RiskAsk, Command: segment, Reason: "discards local changes"}, true
	}
	return CommandVerdict{}, false
}

// isDangerousTarget catches deletes aimed at a root or a home directory.
func isDangerousTarget(target string) bool {
	t := strings.TrimSpace(strings.Trim(target, `"'`))
	t = strings.TrimSuffix(t, "/*")
	t = strings.TrimSuffix(t, "/")
	switch t {
	case "/", "", "~", "$HOME", "/*", ".", "..", "/usr", "/etc", "/var", "/home", "/Users", "C:", `C:\`:
		return true
	}
	return false
}

// escapesWorkspace reports whether a path points outside the project.
func escapesWorkspace(target string) bool {
	t := strings.Trim(target, `"'`)
	return strings.HasPrefix(t, "/") || strings.HasPrefix(t, "~") ||
		strings.HasPrefix(t, "../") || t == ".." || strings.HasPrefix(t, `\\`) ||
		(len(t) > 2 && t[1] == ':')
}

// splitSegments breaks a command line on shell separators so each simple
// command is judged on its own.
func splitSegments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n", "&", "\n")
	var out []string
	for _, part := range strings.Split(replacer.Replace(command), "\n") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// shellPayload returns the command string passed to `sh -c`.
func shellPayload(args []string) string {
	for i, a := range args {
		if (a == "-c" || a == "-Command" || a == "/c" || a == "/C") && i+1 < len(args) {
			return strings.Trim(strings.Join(args[i+1:], " "), `"'`)
		}
	}
	return ""
}

// binaryName reduces a path to the executable name it invokes.
func binaryName(token string) string {
	t := strings.Trim(strings.TrimSpace(token), `"'`)
	if i := strings.LastIndexAny(t, `/\`); i >= 0 {
		t = t[i+1:]
	}
	return strings.ToLower(strings.TrimSuffix(t, ".exe"))
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != 's' && r != 'm' {
			return false
		}
	}
	return true
}

func isForkBomb(segment string) bool {
	compact := strings.ReplaceAll(segment, " ", "")
	return strings.Contains(compact, ":(){:|:&};:") || strings.Contains(compact, ":(){:|:&};")
}
