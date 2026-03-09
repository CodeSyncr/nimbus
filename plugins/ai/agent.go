package ai

import "context"

// Agent represents an AI agent with instructions (system prompt) and optional conversation context.
// Similar to Laravel's Agent concept.
type Agent struct {
	Instructions string
	Messages     []Message
	client       *Client
}

// NewAgent creates an agent with the given system instructions.
func NewAgent(instructions string) *Agent {
	return &Agent{
		Instructions: instructions,
		Messages:     nil,
		client:       GetClient(),
	}
}

// WithMessages sets the conversation history for the agent.
func (a *Agent) WithMessages(msgs []Message) *Agent {
	a.Messages = msgs
	return a
}

// Prompt sends a user message to the agent and returns the completion.
func (a *Agent) Prompt(ctx context.Context, userMessage string, opts ...GenerateOption) (*GenerateResponse, error) {
	msgs := make([]Message, 0, len(a.Messages)+1)
	msgs = append(msgs, a.Messages...)
	msgs = append(msgs, Message{Role: "user", Content: userMessage})

	req := &GenerateRequest{
		Messages:  msgs,
		System:    a.Instructions,
		MaxTokens: 1024,
	}
	for _, opt := range opts {
		opt(req)
	}
	return a.client.GenerateRequest(ctx, req)
}

// Stream produces a streaming response for the given user message.
func (a *Agent) Stream(ctx context.Context, userMessage string, opts ...GenerateOption) (<-chan string, <-chan error) {
	msgs := make([]Message, 0, len(a.Messages)+1)
	msgs = append(msgs, a.Messages...)
	msgs = append(msgs, Message{Role: "user", Content: userMessage})

	req := &GenerateRequest{
		Messages:  msgs,
		System:    a.Instructions,
		MaxTokens: 1024,
	}
	for _, opt := range opts {
		opt(req)
	}
	return a.client.StreamRequest(ctx, req)
}
