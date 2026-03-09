package ai

// Config holds AI plugin configuration.
// API keys are read from environment variables (see README).
type Config struct {
	Provider     string
	Model        string
	Timeout      int
	MaxTokens    int
	// Text generation providers
	OpenAIKey    string
	AnthropicKey string
	CohereKey    string
	GeminiKey    string
	MistralKey   string
	XAIKey       string
	// Ollama uses OLLAMA_HOST (default localhost:11434), no key
	OllamaHost string
	// Embeddings / specialized (for future use)
	JinaKey      string
	VoyageAIKey  string
	ElevenLabsKey string
}
