package tui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/internal/ai"
)

/*
Rebuilding the visible transcript when a session resumes.

The session file carries the whole conversation, which is what lets the model
pick up where it left off. The screen, though, was built empty every time: a
resumed session had full context and a blank window, so it looked as if nothing
had been remembered.

This turns the stored conversation back into the chat items the view renders.

The content of a stored message is not typed the way it was in memory — a
session that has been through the JSON file arrives as []any of map[string]any
rather than []ContentBlock — so every shape is decoded here.
*/

// toTranscriptEntry converts a displayed line into its stored form.
func toTranscriptEntry(item ChatItem) ai.TranscriptEntry {
	args := map[string]string{}
	for k, v := range item.ToolArgs {
		if s, ok := v.(string); ok {
			args[k] = s
		}
	}
	if len(args) == 0 {
		args = nil
	}
	return ai.TranscriptEntry{
		Role:      item.Role,
		Content:   item.Content,
		ToolName:  item.ToolName,
		ToolArgs:  args,
		Detail:    item.Detail,
		Diff:      item.Diff,
		IsError:   item.IsError,
		ElapsedMS: item.Elapsed.Milliseconds(),
		At:        item.Timestamp,
	}
}

// fromTranscriptEntry converts a stored line back for rendering.
func fromTranscriptEntry(e ai.TranscriptEntry) ChatItem {
	args := map[string]any{}
	for k, v := range e.ToolArgs {
		args[k] = v
	}
	if len(args) == 0 {
		args = nil
	}
	return ChatItem{
		Role:      e.Role,
		Content:   e.Content,
		ToolName:  e.ToolName,
		ToolArgs:  args,
		Detail:    e.Detail,
		Diff:      e.Diff,
		IsError:   e.IsError,
		Elapsed:   time.Duration(e.ElapsedMS) * time.Millisecond,
		Timestamp: e.At,
	}
}

// restoreTranscript rebuilds what the user saw.
//
// The stored transcript is preferred: it is append-only, so it survives the
// compaction and pruning that rewrite the model's context. Sessions saved
// before the transcript existed fall back to reconstructing from those
// messages, which is lossy but better than an empty window.
func restoreTranscript(session *ai.Session) []ChatItem {
	if session == nil {
		return make([]ChatItem, 0)
	}
	if len(session.Transcript) > 0 {
		items := make([]ChatItem, 0, len(session.Transcript))
		for _, e := range session.Transcript {
			items = append(items, fromTranscriptEntry(e))
		}
		return items
	}
	if len(session.Messages) == 0 {
		return make([]ChatItem, 0)
	}

	items := make([]ChatItem, 0, len(session.Messages))
	stamp := session.UpdatedAt
	if stamp.IsZero() {
		stamp = time.Now()
	}

	for _, msg := range session.Messages {
		blocks, text := decodeContent(msg.Content)

		// A plain string message is a turn of the dialogue.
		if len(blocks) == 0 {
			if strings.TrimSpace(text) == "" {
				continue
			}
			items = append(items, ChatItem{
				Role:      restoredRole(msg.Role, text),
				Content:   text,
				Timestamp: stamp,
			})
			continue
		}

		for _, b := range blocks {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) == "" {
					continue
				}
				items = append(items, ChatItem{Role: "assistant", Content: b.Text, Timestamp: stamp})

			case "tool_use":
				items = append(items, ChatItem{
					Role:      "tool",
					ToolName:  b.Name,
					ToolArgs:  b.Input,
					Content:   toolTarget(b.Input),
					Detail:    "",
					Timestamp: stamp,
				})

			case "tool_result":
				// Results belong to the tool line above them rather than to a
				// line of their own, which is how they render live.
				attachResult(items, b)
			}
		}
	}
	return items
}

// restoredRole keeps the agent's own injected messages out of the user's
// mouth. A verification failure or a trim notice is machinery, not something
// the user typed.
func restoredRole(role, text string) string {
	if role != "user" {
		return role
	}
	switch {
	case strings.HasPrefix(text, "VERIFICATION FAILED"),
		strings.HasPrefix(text, "[earlier turns"):
		return "system"
	}
	return "user"
}

// attachResult records a tool result against the most recent tool line.
func attachResult(items []ChatItem, b ai.ContentBlock) {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Role != "tool" || items[i].Detail != "" {
			continue
		}
		if b.IsError {
			items[i].IsError = true
			items[i].Detail = firstLineOf(b.Content)
			return
		}
		items[i].Detail = summariseResult(items[i].ToolName, b.Content)
		return
	}
}

// summariseResult reduces a stored tool result to the short label the live
// view shows, so a resumed transcript reads the same as the original.
func summariseResult(toolName, content string) string {
	if detail := toolDetail(toolName, content, nil); detail != "" {
		return detail
	}
	return "done"
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// decodeContent normalises a stored message body into blocks or plain text.
//
// In memory the content is a string or []ContentBlock; loaded from the session
// file the same data arrives as []any of map[string]any.
func decodeContent(content any) ([]ai.ContentBlock, string) {
	switch v := content.(type) {
	case nil:
		return nil, ""
	case string:
		return nil, v
	case []ai.ContentBlock:
		return v, ""
	case []any:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, ""
		}
		var blocks []ai.ContentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return nil, ""
		}
		return blocks, ""
	}
	return nil, ""
}
