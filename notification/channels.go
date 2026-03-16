package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ── Slack Channel ───────────────────────────────────────────────

// SlackNotification is for notifications that support the Slack channel.
type SlackNotification interface {
	Notification
	// ToSlack returns the Slack message, or nil to skip.
	ToSlack() *SlackMessage
}

// SlackMessage represents a Slack webhook message.
type SlackMessage struct {
	Channel     string            `json:"channel,omitempty"`
	Text        string            `json:"text,omitempty"`
	Username    string            `json:"username,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

// SlackAttachment represents a Slack message attachment.
type SlackAttachment struct {
	Color    string       `json:"color,omitempty"`
	Title    string       `json:"title,omitempty"`
	Text     string       `json:"text,omitempty"`
	Fields   []SlackField `json:"fields,omitempty"`
	Footer   string       `json:"footer,omitempty"`
	Fallback string       `json:"fallback,omitempty"`
}

// SlackField is a key-value field in a Slack attachment.
type SlackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// SlackChannel sends notifications via Slack incoming webhooks.
type SlackChannel struct {
	WebhookURL string
}

// NewSlackChannel creates a Slack channel with the given webhook URL.
func NewSlackChannel(webhookURL string) *SlackChannel {
	return &SlackChannel{WebhookURL: webhookURL}
}

// Send sends the notification to Slack.
func (c *SlackChannel) Send(n SlackNotification) error {
	msg := n.ToSlack()
	if msg == nil {
		return nil
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("notification/slack: marshal: %w", err)
	}

	resp, err := http.Post(c.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notification/slack: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification/slack: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ── Discord Channel ─────────────────────────────────────────────

// DiscordNotification is for notifications that support the Discord channel.
type DiscordNotification interface {
	Notification
	// ToDiscord returns the Discord webhook message, or nil to skip.
	ToDiscord() *DiscordMessage
}

// DiscordMessage represents a Discord webhook message.
type DiscordMessage struct {
	Content   string         `json:"content,omitempty"`
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []DiscordEmbed `json:"embeds,omitempty"`
}

// DiscordEmbed represents an embedded rich content block.
type DiscordEmbed struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	URL         string              `json:"url,omitempty"`
	Color       int                 `json:"color,omitempty"` // decimal color value
	Fields      []DiscordEmbedField `json:"fields,omitempty"`
	Footer      *DiscordEmbedFooter `json:"footer,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"` // ISO8601
}

// DiscordEmbedField is a field within a Discord embed.
type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// DiscordEmbedFooter is the footer of a Discord embed.
type DiscordEmbedFooter struct {
	Text    string `json:"text"`
	IconURL string `json:"icon_url,omitempty"`
}

// DiscordChannel sends notifications via Discord webhooks.
type DiscordChannel struct {
	WebhookURL string
}

// NewDiscordChannel creates a Discord channel with the given webhook URL.
func NewDiscordChannel(webhookURL string) *DiscordChannel {
	return &DiscordChannel{WebhookURL: webhookURL}
}

// Send sends the notification to Discord.
func (c *DiscordChannel) Send(n DiscordNotification) error {
	msg := n.ToDiscord()
	if msg == nil {
		return nil
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("notification/discord: marshal: %w", err)
	}

	resp, err := http.Post(c.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notification/discord: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification/discord: status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
