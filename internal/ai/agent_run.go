package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

/*
The conversational agent loop.

The original design ran every request through a fixed pipeline —
explore → plan → approve → execute — with each phase starting a *fresh*
message list built from a template brief. The dialogue was never sent to the
model, only a reformatted summary of it, which is why:

  - "continue" did nothing: the next message began a brand-new investigation
    with no memory of what had been started;
  - instructions were ignored: the user's words were wrapped in a brief whose
    own instructions dominated them;
  - a question like "what does this project do?" still produced a plan and an
    approval gate, because planning was the only route to the model.

Run replaces that with one conversation that persists for the session. The
model sees every message, tool call and tool result in order, and decides for
itself what a turn needs: answer directly, investigate first, or change code.
Approval is no longer a phase — it is per-command and only for what the
command policy flags (see command_policy.go).

The plan pathway still exists for callers that explicitly want a reviewable
plan (nimbus vani --plan-only); it is no longer the only way through.
*/

// RunResult is the outcome of one conversational turn.
type RunResult struct {
	// Text is the assistant's final message.
	Text string
	// ChangedFiles lists paths written, edited or deleted during the turn.
	ChangedFiles []string
	// ToolCalls counts tool invocations made during the turn.
	ToolCalls int
	// Verified reports whether the project build was run and passed.
	Verified bool
}

// maxRunIterations bounds a single turn's tool loop, counted in model
// round-trips rather than tool calls — a round-trip that calls five tools
// costs one. It is generous: a real feature can legitimately need dozens of
// reads and edits.
const maxRunIterations = 60

// compactRetryGrowth is how much the conversation must grow before another
// compaction is attempted after one that reclaimed nothing. Each attempt costs
// a model round-trip, so retrying on every iteration of a long turn is worse
// than living with a full window.
const compactRetryGrowth = 8000

// Run handles one user message in the session's ongoing conversation.
//
// It appends the message to the persistent history, then loops: ask the model,
// run whatever tools it calls, feed the results back, and repeat until the
// model answers with no further tool calls. Nothing is gated on a plan, and no
// clarification round-trip is imposed — if the model needs to ask something it
// simply says so, and the user's reply is the next message in the same
// conversation.
func (a *Agent) Run(ctx context.Context, userMessage string) (*RunResult, error) {
	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return &RunResult{}, nil
	}

	if a.Session.InitialQuery == "" {
		a.Session.InitialQuery = userMessage
	}
	if a.Context != nil {
		a.Context.RefreshVolatile()
	}

	a.State = StateExecuting
	a.Session.Status = "running"
	a.changedFiles = map[string]bool{}
	a.resetRepeatGuard()
	a.Session.AppendUser(userMessage)

	// Compact before starting when the conversation has grown close to the
	// window, so the turn begins with room to work rather than failing partway
	// through for want of context.
	a.maybeCompact(ctx)

	result := &RunResult{}
	tools := a.Tools.GetToolDefinitions()
	repairRounds := 0
	emptyReplies := 0

	for iter := 0; iter < maxRunIterations; iter++ {
		resp, err := a.turn(ctx, TurnModeAgent, a.Session.Messages, tools, nil)
		// The server asked the model more than once and got nothing. That is
		// not a failed turn yet: say so to the model and ask again, a bounded
		// number of times, with the work so far still in the conversation.
		if err != nil && errors.Is(err, ErrEmptyReply) && emptyReplies < maxEmptyReplies {
			emptyReplies++
			a.status("Nothing came back — asking again…")
			a.Session.AppendUser(emptyReplyNotice)
			continue
		}
		if err != nil {
			// Older servers: either no /ai/turn at all, or no conversational
			// mode (turn() already retried agent → chat). Either way there is
			// no conversational endpoint to talk to, so fall back to the
			// staged pipeline rather than failing the user's request.
			if iter == 0 && (errors.Is(err, ErrTurnUnsupported) || isUnknownModeErr(err)) {
				return a.runStaged(ctx, userMessage)
			}
			a.State = StateFailed
			a.saveSession()
			return nil, err
		}

		// The server counted the request that was just sent, including the
		// tool schemas and project context the local estimate cannot see.
		// Recording it here — before the reply is appended — keeps the
		// measurement aligned with the messages that produced the count.
		if resp.Usage != nil {
			a.Session.RecordContextTokens(resp.Usage.InputTokens)
		}

		toolBlocks := resp.ToolUseBlocks()

		// No tools left to run: the model has answered.
		if len(toolBlocks) == 0 {
			final := strings.TrimSpace(resp.TextContent())

			// Neither words nor a tool call. Reporting that as "Done." told
			// the user work had finished when a request had simply come back
			// blank; ask again first, and say what happened if that fails.
			if final == "" && emptyReplies < maxEmptyReplies {
				emptyReplies++
				a.status("Nothing came back — asking again…")
				a.Session.AppendUser(emptyReplyNotice)
				continue
			}

			// Something the user said while this answer was being written:
			// the model has not seen it, so the answer is not the last word.
			// Keep the answer in the conversation and carry on with the note.
			if final != "" && a.hasSteer() {
				a.Session.AppendAssistant(resp.Content)
				a.readSteer()
				continue
			}

			// Changed code is verified before the turn is called done, and a
			// failing build is handed back for repair rather than reported as
			// success.
			if a.shouldVerify(repairRounds) {
				a.State = StateVerifying
				a.status("Verifying the build…")
				out, ok := a.Verifier(ctx)
				a.State = StateExecuting
				if !ok {
					repairRounds++
					a.status(fmt.Sprintf("Build failed — repairing (%d/%d)…", repairRounds, maxRepairRounds))
					a.Session.AppendAssistant(resp.Content)
					a.Session.AppendUser(fmt.Sprintf(
						"VERIFICATION FAILED. The project does not build after your changes:\n\n%s\n\nRead the errors, fix the root cause, re-run the build, and then reply with the final summary.", out))
					continue
				}
				result.Verified = true
			}

			if final == "" {
				final = emptyReplyFinal
			}
			a.Session.AppendAssistant(resp.Content)
			a.finishRun(userMessage, final, result)
			return result, nil
		}

		// Tools to run: execute them and feed the results back.
		a.Session.AppendAssistant(resp.Content)
		results := a.runToolCalls(ctx, toolBlocks)
		result.ToolCalls += len(toolBlocks)
		if nudge := a.repeatNudge(toolBlocks); nudge != "" {
			results = append(results, ContentBlock{Type: "text", Text: nudge})
		}
		a.Session.AppendToolResults(results)

		// What the user said while this step ran is read now, after its tools
		// have answered and before the model is asked again.
		a.readSteer()

		// Tool results are where a turn's context actually goes, so the check
		// belongs here and not only at the top of Run. Checking once per turn
		// let a single request run sixty rounds past the threshold: the window
		// filled, stayed full for the rest of the turn, and only came back
		// down when the *next* message reached the check above.
		a.maybeCompact(ctx)
	}

	final := fmt.Sprintf(
		"I stopped after %d model round-trips (%d tool calls), which is the per-turn cap. Everything done so far is saved — say 'continue' to carry on from here.",
		maxRunIterations, result.ToolCalls)
	a.finishRun(userMessage, final, result)
	return result, nil
}

// maybeCompact compacts the conversation when it has passed the threshold.
//
// Compaction is an optimisation, so nothing here is allowed to fail the user's
// request: an error is reported as status and the turn continues.
//
// A compaction that reclaims nothing is remembered. It happens when the
// messages kept verbatim are themselves over the threshold, and without the
// guard the threshold stays tripped and every subsequent iteration pays for
// another summary that cannot help.
func (a *Agent) maybeCompact(ctx context.Context) {
	if a.Session == nil {
		return
	}
	if !a.settings.AutoCompact {
		return
	}
	usage := a.Session.ContextUsage()
	if !usage.NeedsCompaction() {
		a.compactStalledAt = 0
		return
	}
	if a.compactStalledAt > 0 && usage.Tokens < a.compactStalledAt+compactRetryGrowth {
		return
	}

	res, err := a.Compact(ctx)
	if err != nil {
		a.status("Could not compact the conversation; continuing")
		a.compactStalledAt = usage.Tokens
		return
	}
	if res.Saved() == 0 {
		a.compactStalledAt = usage.Tokens
		return
	}

	a.compactStalledAt = 0
	a.status(fmt.Sprintf("Compacted %d earlier messages, freeing %s tokens.",
		res.Summarised, FormatTokens(res.Saved())))
}

// runStaged drives the legacy explore → plan → execute endpoints for servers
// that predate /ai/turn. The plan is executed directly: those servers have no
// conversational mode, and this is what the CLI did for them before.
func (a *Agent) runStaged(ctx context.Context, userMessage string) (*RunResult, error) {
	plan, err := a.GeneratePlan(ctx, userMessage)
	if err != nil {
		return nil, err
	}

	result := &RunResult{}
	if plan == nil || len(plan.Steps) == 0 {
		summary := "Done."
		if plan != nil && strings.TrimSpace(plan.Summary) != "" {
			summary = plan.Summary
		}
		result.Text = summary
		return result, nil
	}

	summary, err := a.ExecuteApprovedPlan(ctx, plan)
	if err != nil {
		return nil, err
	}
	result.Text = summary
	result.ChangedFiles = a.changedFileList()
	return result, nil
}

// shouldVerify reports whether the build should be checked before finishing.
func (a *Agent) shouldVerify(repairRounds int) bool {
	return a.Verifier != nil && len(a.changedFiles) > 0 && repairRounds < maxRepairRounds
}

// finishRun records the turn and persists the session so a later --resume (or
// a "continue") picks up exactly where this left off.
func (a *Agent) finishRun(userMessage, final string, result *RunResult) {
	result.Text = final
	result.ChangedFiles = a.changedFileList()

	a.State = StateCompleted
	a.Session.Status = "completed"
	a.Session.RecordTurn(userMessage, "", final, result.ChangedFiles)
	if a.Context != nil {
		a.Context.RefreshVolatile()
	}
	if a.Callbacks.OnExecutionCompleted != nil {
		a.Callbacks.OnExecutionCompleted(final)
	}
	a.saveSession()
}

// ---------------------------------------------------------------------------
// Replies that did not arrive whole
// ---------------------------------------------------------------------------

// maxEmptyReplies is how many times in one turn an empty reply is asked for
// again. Once is a hiccup; a model that keeps doing it is not going to answer,
// and each attempt is a full request.
const maxEmptyReplies = 2

// emptyReplyNotice is what the model is told after sending back nothing.
const emptyReplyNotice = "NOTE: your last reply was empty. Carry on from where you were: make the next tool call, or write your answer."

// emptyReplyFinal is what the user is told when asking again did not help.
// It replaces a bare "Done.", which claimed work that had not happened.
const emptyReplyFinal = "The model sent back an empty reply, and did so again when asked. Nothing further was done — say 'continue' to try again."

// cutOffMessage answers a tool call that was not run because its arguments
// were incomplete.
const cutOffMessage = "this call was not run: the reply reached the output limit while its arguments were still being written, so they arrived incomplete. Nothing was changed. " +
	"Do not send the same call again — it will be cut at the same place. Write less in one call: change the file with edit_file instead of rewriting it, " +
	"or write the first part with write_file and add the rest with edit_file, a part at a time."

// truncatedStop reports whether a stop reason means the reply was cut at the
// output limit. Anthropic-shaped servers say "max_tokens"; OpenAI-shaped ones
// say "length".
func truncatedStop(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "max_tokens", "length", "max_output_tokens":
		return true
	}
	return false
}

// cutOff reports whether a tool call's arguments are known to be incomplete.
//
// A current server marks such a call. An older one parses what it can of the
// arguments and, failing, sends the call with none at all — so a call with no
// arguments in a reply that stopped at the output limit is the same thing, and
// is treated the same. Running it would mean writing a file with no path, or
// worse, the first half of one.
func (a *Agent) cutOff(tb ContentBlock) bool {
	if tb.Incomplete {
		return true
	}
	return truncatedStop(a.lastStop) && len(tb.Input) == 0
}

// ---------------------------------------------------------------------------
// Steering
// ---------------------------------------------------------------------------

// Steer hands the agent something the user said while it was working.
//
// It is not sent at once: a message cannot be put between a tool call and its
// result. The loop shows it to the model at the next point where it can be —
// after the tools of the current step have answered and before the next model
// call — so the work carries on with it in mind instead of being interrupted
// and started again. Safe to call from another goroutine.
func (a *Agent) Steer(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.steerMu.Lock()
	a.steer = append(a.steer, text)
	a.steerMu.Unlock()
}

// TakeSteer returns, and forgets, what was said but never shown to the model:
// the turn ended first. The caller sends it as the next message.
func (a *Agent) TakeSteer() []string {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	out := a.steer
	a.steer = nil
	return out
}

// SteerWaiting is how many messages are waiting to be shown to the model.
func (a *Agent) SteerWaiting() int {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	return len(a.steer)
}

// hasSteer reports whether anything is waiting to be shown to the model.
func (a *Agent) hasSteer() bool { return a.SteerWaiting() > 0 }

// steerNote is how a mid-turn message is put to the model.
func steerNote(text string) string {
	return "The user added this while you were working. Take it into account from here on:\n\n" + text
}

// readSteer appends what the user said since the last step to the
// conversation. It reports whether there was anything.
func (a *Agent) readSteer() bool {
	notes := a.TakeSteer()
	for _, note := range notes {
		a.Session.AppendUser(steerNote(note))
		if a.Callbacks.OnSteered != nil {
			a.Callbacks.OnSteered(note)
		}
	}
	return len(notes) > 0
}
