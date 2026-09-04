package ai

// Config holds AI plugin configuration.
// API keys are read from environment variables (see README).
type Config struct {
	Provider string
	Model    string
	// ImageProvider and ImageModel configure image generation separately from
	// text. Images come from a different endpoint, and the best image model is
	// rarely from the same family as the text model — so "gemini for pictures,
	// something else for reasoning" is a supported combination.
	//
	// Resolution order for the model: ai.Image().Model(…) → ImageModel →
	// the provider's default.
	ImageProvider string
	ImageModel    string
	Timeout       int
	MaxTokens     int
	// Text generation providers
	OpenAIKey        string
	OpenAIBaseURL    string
	AnthropicKey     string
	AnthropicBaseURL string
	AnthropicAPIURL  string
	CohereKey        string
	GeminiKey        string
	MistralKey       string
	XAIKey           string
	// Ollama uses OLLAMA_HOST (default localhost:11434), no key
	OllamaHost string
	// Embeddings / specialized (for future use)
	JinaKey       string
	VoyageAIKey   string
	ElevenLabsKey string
}
