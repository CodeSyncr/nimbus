/*
|--------------------------------------------------------------------------
| AI SDK — Speech: transcription and text-to-speech
|--------------------------------------------------------------------------
|
|   t, err := ai.Transcribe(ctx, "call.mp3")                      // path, URL or data: URI
|   t.Text; t.Language; t.Segments
|
|   audio, err := ai.Speak(ctx, "Your order has shipped.", ai.WithVoice("coral"))
|   os.WriteFile("reply.mp3", audio.Data, 0o644)
|
| OpenAI (and OpenAI-compatible hosts such as Groq for Whisper) does both;
| Gemini transcribes with its text models and speaks with its TTS models.
|
*/

package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// Transcription is speech turned into text.
type Transcription struct {
	Text     string
	Language string
	Duration float64 // seconds, when the provider reports it
	Segments []TranscriptSegment
}

// TranscriptSegment is a timed piece of a transcription.
type TranscriptSegment struct {
	Start, End float64 // seconds
	Text       string
}

// TranscriptionRequest configures Transcribe.
type TranscriptionRequest struct {
	Audio    Attachment
	Model    string
	Language string // ISO-639-1 hint, e.g. "en"
	Prompt   string // vocabulary or context to steer spelling
}

// Speech is synthesised audio.
type Speech struct {
	Data      []byte
	MediaType string // audio/mpeg, audio/wav, …
}

// SpeechRequest configures Speak.
type SpeechRequest struct {
	Text         string
	Model        string
	Voice        string
	Format       string // mp3 (default), wav, opus, aac, flac, pcm
	Speed        float64
	Instructions string // tone and delivery, for models that take it
}

// TranscriptionProvider turns audio into text.
type TranscriptionProvider interface {
	Transcribe(ctx context.Context, req *TranscriptionRequest) (*Transcription, error)
}

// SpeechProvider turns text into audio.
type SpeechProvider interface {
	Speak(ctx context.Context, req *SpeechRequest) (*Speech, error)
}

// TranscribeOption configures Transcribe.
type TranscribeOption func(*TranscriptionRequest)

// WithLanguage hints the spoken language (ISO-639-1).
func WithLanguage(lang string) TranscribeOption {
	return func(r *TranscriptionRequest) { r.Language = lang }
}

// WithTranscriptionModel picks the model (default whisper-1 on OpenAI).
func WithTranscriptionModel(model string) TranscribeOption {
	return func(r *TranscriptionRequest) { r.Model = model }
}

// WithTranscriptionPrompt passes vocabulary or context that steers spelling.
func WithTranscriptionPrompt(prompt string) TranscribeOption {
	return func(r *TranscriptionRequest) { r.Prompt = prompt }
}

// SpeakOption configures Speak.
type SpeakOption func(*SpeechRequest)

// WithVoice picks the voice (OpenAI: alloy, coral, nova, …; Gemini: Kore,
// Puck, …).
func WithVoice(voice string) SpeakOption { return func(r *SpeechRequest) { r.Voice = voice } }

// WithSpeechModel picks the model (default gpt-4o-mini-tts on OpenAI).
func WithSpeechModel(model string) SpeakOption { return func(r *SpeechRequest) { r.Model = model } }

// WithAudioFormat picks the output format (mp3, wav, opus, aac, flac, pcm).
func WithAudioFormat(format string) SpeakOption { return func(r *SpeechRequest) { r.Format = format } }

// WithSpeechInstructions describes tone and delivery ("calm, warm").
func WithSpeechInstructions(text string) SpeakOption {
	return func(r *SpeechRequest) { r.Instructions = text }
}

// WithSpeed sets the speaking rate (0.25–4, default 1).
func WithSpeed(speed float64) SpeakOption { return func(r *SpeechRequest) { r.Speed = speed } }

// Transcribe turns audio into text. audio is a path, URL or data: URI.
func (c *Client) Transcribe(ctx context.Context, audio string, opts ...TranscribeOption) (*Transcription, error) {
	a, err := LoadAttachment(ctx, audio)
	if err != nil {
		return nil, err
	}
	return c.TranscribeAudio(ctx, a, opts...)
}

// TranscribeAudio is Transcribe for audio already in memory.
func (c *Client) TranscribeAudio(ctx context.Context, audio Attachment, opts ...TranscribeOption) (*Transcription, error) {
	tp, ok := c.provider.(TranscriptionProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %s cannot transcribe audio", c.provider.Name())
	}
	req := &TranscriptionRequest{Audio: audio}
	for _, o := range opts {
		o(req)
	}
	return withRetries(ctx, c.maxRetries(), func() (*Transcription, error) { return tp.Transcribe(ctx, req) })
}

// Speak turns text into audio.
func (c *Client) Speak(ctx context.Context, text string, opts ...SpeakOption) (*Speech, error) {
	sp, ok := c.provider.(SpeechProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %s cannot synthesise speech", c.provider.Name())
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("ai: nothing to speak")
	}
	req := &SpeechRequest{Text: text}
	for _, o := range opts {
		o(req)
	}
	return withRetries(ctx, c.maxRetries(), func() (*Speech, error) { return sp.Speak(ctx, req) })
}

// Transcribe uses the global client.
func Transcribe(ctx context.Context, audio string, opts ...TranscribeOption) (*Transcription, error) {
	return GetClient().Transcribe(ctx, audio, opts...)
}

// Speak uses the global client.
func Speak(ctx context.Context, text string, opts ...SpeakOption) (*Speech, error) {
	return GetClient().Speak(ctx, text, opts...)
}

// ── OpenAI ──────────────────────────────────────────────────────────

func (p *openAIProvider) Transcribe(ctx context.Context, req *TranscriptionRequest) (*Transcription, error) {
	model := req.Model
	if model == "" {
		model = openai.Whisper1
	}
	name := req.Audio.Name
	if name == "" {
		name = "audio" + audioExt(req.Audio.MediaType)
	}
	format := openai.AudioResponseFormatVerboseJSON
	if model != openai.Whisper1 {
		format = openai.AudioResponseFormatJSON // the gpt-4o transcribe models take json only
	}
	resp, err := p.client.CreateTranscription(ctx, openai.AudioRequest{
		Model:    model,
		FilePath: name,
		Reader:   bytes.NewReader(req.Audio.Data),
		Prompt:   req.Prompt,
		Language: req.Language,
		Format:   format,
	})
	if err != nil {
		return nil, fmt.Errorf("ai: openai transcription: %w", err)
	}
	t := &Transcription{Text: strings.TrimSpace(resp.Text), Language: resp.Language, Duration: resp.Duration}
	for _, s := range resp.Segments {
		t.Segments = append(t.Segments, TranscriptSegment{Start: s.Start, End: s.End, Text: strings.TrimSpace(s.Text)})
	}
	return t, nil
}

func (p *openAIProvider) Speak(ctx context.Context, req *SpeechRequest) (*Speech, error) {
	model := req.Model
	if model == "" {
		model = "gpt-4o-mini-tts"
	}
	voice := req.Voice
	if voice == "" {
		voice = "alloy"
	}
	format := req.Format
	if format == "" {
		format = "mp3"
	}
	r := openai.CreateSpeechRequest{
		Model:          openai.SpeechModel(model),
		Input:          req.Text,
		Voice:          openai.SpeechVoice(voice),
		ResponseFormat: openai.SpeechResponseFormat(format),
		Speed:          req.Speed,
	}
	if !strings.HasPrefix(model, "tts-1") {
		r.Instructions = req.Instructions
	}
	raw, err := p.client.CreateSpeech(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("ai: openai speech: %w", err)
	}
	defer raw.Close()
	data, err := io.ReadAll(raw)
	if err != nil {
		return nil, err
	}
	return &Speech{Data: data, MediaType: audioMediaType(format)}, nil
}

// ── Gemini ──────────────────────────────────────────────────────────

func (p *geminiProvider) Transcribe(ctx context.Context, req *TranscriptionRequest) (*Transcription, error) {
	instr := "Transcribe this audio verbatim. Reply with the transcript only."
	if req.Language != "" {
		instr += " The speech is in " + req.Language + "."
	}
	if req.Prompt != "" {
		instr += " Context: " + req.Prompt
	}
	resp, err := p.Generate(ctx, &GenerateRequest{
		Model: req.Model,
		Messages: []Message{{Role: RoleUser, Content: instr,
			Files: []string{req.Audio.DataURI()}}},
		Temperature: 0,
	})
	if err != nil {
		return nil, err
	}
	return &Transcription{Text: strings.TrimSpace(resp.Text), Language: req.Language}, nil
}

func (p *geminiProvider) Speak(ctx context.Context, req *SpeechRequest) (*Speech, error) {
	model := req.Model
	if model == "" {
		model = "gemini-2.5-flash-preview-tts"
	}
	voice := req.Voice
	if voice == "" {
		voice = "Kore"
	}
	text := req.Text
	if req.Instructions != "" {
		text = req.Instructions + ": " + text
	}
	body := map[string]any{
		"contents": []any{map[string]any{"parts": []any{map[string]any{"text": text}}}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"AUDIO"},
			"speechConfig": map[string]any{
				"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": voice}},
			},
		},
	}
	raw, err := p.postJSON(ctx, p.imageBaseURL()+"/models/"+model+":generateContent", body)
	if err != nil {
		return nil, err
	}
	var gr geminiResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return nil, fmt.Errorf("gemini: parse speech: %w", err)
	}
	for _, c := range gr.Candidates {
		for _, part := range c.Content.Parts {
			if part.InlineData == nil {
				continue
			}
			pcm, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				return nil, fmt.Errorf("gemini: speech audio: %w", err)
			}
			// Gemini returns raw 16-bit 24 kHz mono PCM; wrap it as WAV.
			return &Speech{Data: wavFromPCM(pcm, 24000, 1, 16), MediaType: "audio/wav"}, nil
		}
	}
	return nil, errors.New("gemini: no audio in the response")
}

func (p *geminiProvider) postJSON(ctx context.Context, url string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := newJSONRequest(ctx, url, b)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-goog-api-key", p.apiKey)
	resp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, newAPIError("gemini", resp)
	}
	return io.ReadAll(resp.Body)
}

// wavFromPCM wraps little-endian PCM samples in a WAV header.
func wavFromPCM(pcm []byte, sampleRate, channels, bits int) []byte {
	var b bytes.Buffer
	byteRate := sampleRate * channels * bits / 8
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels*bits/8))
	_ = binary.Write(&b, binary.LittleEndian, uint16(bits))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

func audioMediaType(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "pcm":
		return "audio/L16"
	default:
		return "audio/mpeg"
	}
}

func audioExt(mediaType string) string {
	switch mediaType {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/webm":
		return ".webm"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/flac":
		return ".flac"
	default:
		return ".mp3"
	}
}

var (
	_ TranscriptionProvider = (*openAIProvider)(nil)
	_ SpeechProvider        = (*openAIProvider)(nil)
	_ TranscriptionProvider = (*geminiProvider)(nil)
	_ SpeechProvider        = (*geminiProvider)(nil)
)

func newJSONRequest(ctx context.Context, url string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
