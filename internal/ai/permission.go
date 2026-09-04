package ai

import (
	"fmt"
	"path/filepath"
	"strings"
)

/*
Auto mode.

Every tool call is assessed before it runs. Calls judged low-risk go straight
through, consequential ones ask, and a few are refused outright — so ordinary
work is not interrupted and the moments that deserve a human are the only ones
that get one.

Two things are weighed:

  what the call does      writing to .env or .git, deleting outside the
                          project, a shell command that uploads files or
                          executes something fetched from the network

  where the idea came     content the agent read from outside — a fetched
                          page, a file in the repo — can contain text aimed at
                          the agent rather than at a human reader. A tool call
                          made straight after reading such content is not
                          trusted the way the user's own instruction is.

The modes:

  auto    assess every call; run the safe ones, ask about the rest (default)
  ask     ask before anything that writes, deletes or runs a command
  allow   run everything the policy does not refuse outright (nimbus ai --yes)

Nothing here is a security boundary — the agent runs with the user's own
permissions and a determined model could phrase its way around any list. It is
the guardrail that keeps a plausible mistake from becoming an expensive one.
*/

// PermissionMode selects how much is decided without asking.
type PermissionMode string

const (
	// PermissionAuto assesses each call and asks only about consequential ones.
	PermissionAuto PermissionMode = "auto"
	// PermissionAsk confirms anything that changes the workspace.
	PermissionAsk PermissionMode = "ask"
	// PermissionAllow runs whatever the policy does not refuse.
	PermissionAllow PermissionMode = "allow"
)

// ParsePermissionMode reads a mode name, falling back to auto.
func ParsePermissionMode(s string) PermissionMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ask", "confirm", "prompt":
		return PermissionAsk
	case "allow", "yes", "yolo", "auto-approve":
		return PermissionAllow
	default:
		return PermissionAuto
	}
}

// Assessment is the verdict on one tool call.
type Assessment struct {
	Risk    CommandRisk
	Reason  string // shown in the approval prompt or the refusal
	Subject string // the command or path the verdict is about
}

// sensitivePaths are files whose contents are credentials, history, or the
// machinery of the repository itself. Editing them is legitimate but rarely
// what someone meant by "add a feature".
var sensitivePaths = []struct {
	match  func(rel string) bool
	reason string
}{
	{func(rel string) bool { return rel == ".env" || strings.HasPrefix(rel, ".env.") }, "contains credentials"},
	{func(rel string) bool { return strings.HasPrefix(rel, ".git/") }, "is git's internal state"},
	{func(rel string) bool { return strings.HasPrefix(rel, ".github/workflows/") }, "runs in CI with repository secrets"},
	{func(rel string) bool { return strings.HasPrefix(rel, ".ssh/") || strings.Contains(rel, "id_rsa") }, "is an SSH key"},
	{func(rel string) bool { return rel == ".npmrc" || rel == ".pypirc" || rel == ".netrc" }, "holds registry credentials"},
	{func(rel string) bool { return strings.HasSuffix(rel, ".pem") || strings.HasSuffix(rel, ".key") }, "is a private key"},
	{func(rel string) bool { return rel == "Dockerfile" || rel == "docker-compose.yml" }, "defines how the app is deployed"},
}

// AssessToolCall judges one tool call before it runs.
func (t *ToolExecutor) AssessToolCall(name string, args map[string]any) Assessment {
	switch strings.ToLower(name) {
	case "bash", "run_command", "command", "shell":
		v := ClassifyCommand(strArg(args, "command"))
		return t.applyTaint(Assessment{Risk: v.Risk, Reason: v.Reason, Subject: v.Command}, "command")

	case "write_file", "create_file", "create", "write", "edit_file", "edit":
		return t.applyTaint(t.assessWrite(strArg(args, "path")), "edit")

	case "delete_file", "delete":
		a := t.assessWrite(strArg(args, "path"))
		if a.Risk == RiskAllowed {
			// Deleting is not undoable from inside the agent, but deleting a
			// file it just created is routine; asking every time would make
			// auto mode useless.
			return t.applyTaint(a, "delete")
		}
		return t.applyTaint(a, "delete")

	case "generate_image":
		return t.applyTaint(t.assessWrite(strArg(args, "path")), "write")
	}

	// Reading, searching, listing and fetching change nothing.
	return Assessment{Risk: RiskAllowed}
}

// assessWrite judges a change to one path.
func (t *ToolExecutor) assessWrite(rel string) Assessment {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return Assessment{Risk: RiskAllowed}
	}

	// Outside the workspace is refused by resolvePath already; this catches
	// the attempt early and explains it.
	full, err := t.resolvePath(rel)
	if err != nil {
		return Assessment{Risk: RiskBlocked, Subject: rel, Reason: "is outside the project"}
	}
	clean := filepath.ToSlash(t.relPath(full))

	for _, s := range sensitivePaths {
		if s.match(clean) {
			return Assessment{Risk: RiskAsk, Subject: clean, Reason: s.reason}
		}
	}
	if t.mode == PermissionAsk {
		return Assessment{Risk: RiskAsk, Subject: clean, Reason: "changes a file"}
	}
	return Assessment{Risk: RiskAllowed}
}

// applyTaint escalates a verdict when the agent has recently read content that
// tried to instruct it.
//
// A call is not refused for being tainted — the content may be harmless and
// the model may have ignored it — but it stops being automatic. The user sees
// where the instruction came from and decides.
func (t *ToolExecutor) applyTaint(a Assessment, verb string) Assessment {
	tainted, source, evidence := t.Tainted()
	if !tainted || a.Risk == RiskBlocked {
		return a
	}
	if a.Risk == RiskAllowed {
		a.Risk = RiskAsk
		a.Reason = fmt.Sprintf("%s follows content from %s that tried to give instructions (%q)",
			verb, source, evidence)
		return a
	}
	a.Reason = fmt.Sprintf("%s — and it follows content from %s that tried to give instructions (%q)",
		a.Reason, source, evidence)
	return a
}

// bashTools are assessed inside runCommand instead of in ExecuteTool, so that
// callers reaching Bash directly are covered too.
func isBashTool(name string) bool {
	switch strings.ToLower(name) {
	case "bash", "run_command", "command", "shell":
		return true
	}
	return false
}

// Authorize runs the assessment and obtains consent when one is needed.
func (t *ToolExecutor) Authorize(name string, args map[string]any) error {
	a := t.AssessToolCall(name, args)

	switch a.Risk {
	case RiskBlocked:
		return fmt.Errorf("refused: %s %s", subjectOrTool(a, name), a.Reason)

	case RiskAsk:
		if t.mode == PermissionAllow || t.AutoApprove {
			return nil
		}
		if t.ApproveCommand == nil {
			return fmt.Errorf(
				"refused: %s %s. This needs approval; re-run interactively or pass --yes to allow it",
				subjectOrTool(a, name), a.Reason)
		}
		if !t.ApproveCommand(subjectOrTool(a, name), a.Reason) {
			return fmt.Errorf("declined by the user: %s", a.Reason)
		}
	}
	return nil
}

func subjectOrTool(a Assessment, tool string) string {
	if a.Subject != "" {
		return a.Subject
	}
	return tool
}
