package ai

import (
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestOpenAIMessagesCarryImages(t *testing.T) {
	req := &GenerateRequest{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "recreate this"}},
		Images:   []string{"data:image/png;base64,iVBORw0KGgo=", "https://example.com/a.jpg", "no/such/file.png"},
	}
	attachRequestImages(req)
	if len(req.Images) != 0 || len(req.Messages[0].Images) != 3 {
		t.Fatalf("images not moved onto the user message: %+v", req)
	}
	msgs := (&openAIProvider{}).toOpenAIMessages(req)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages", len(msgs))
	}
	u := msgs[1]
	if u.Content != "" || len(u.MultiContent) != 3 {
		t.Fatalf("user turn should be text + 2 images, got content=%q parts=%+v", u.Content, u.MultiContent)
	}
	if u.MultiContent[0].Type != openai.ChatMessagePartTypeText || u.MultiContent[0].Text != "recreate this" {
		t.Errorf("first part: %+v", u.MultiContent[0])
	}
	if u.MultiContent[1].ImageURL == nil || u.MultiContent[1].ImageURL.URL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Errorf("data URI part: %+v", u.MultiContent[1])
	}
	if u.MultiContent[2].ImageURL.URL != "https://example.com/a.jpg" {
		t.Errorf("url part: %+v", u.MultiContent[2])
	}
	// A plain turn stays a plain string.
	plain := (&openAIProvider{}).toOpenAIMessages(&GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if plain[0].Content != "hi" || plain[0].MultiContent != nil {
		t.Errorf("plain turn changed: %+v", plain[0])
	}
}
