package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tinyWAV = "data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEAQB8AAIA+AAACABAAZGF0YQAAAAA="

func TestOpenAITranscribeAndSpeak(t *testing.T) {
	var gotModel, gotFile, speechBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			_ = r.ParseMultipartForm(1 << 20)
			gotModel = r.FormValue("model")
			if _, fh, err := r.FormFile("file"); err == nil {
				gotFile = fh.Filename
			}
			_, _ = w.Write([]byte(`{"task":"transcribe","language":"english","duration":2.5,"text":" Hello there. ","segments":[{"start":0,"end":2.5,"text":" Hello there."}]}`))
		case strings.HasSuffix(r.URL.Path, "/audio/speech"):
			b, _ := io.ReadAll(r.Body)
			speechBody = string(b)
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("ID3fake-mp3"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p, err := newOpenAIProvider(&Config{OpenAIKey: "k", OpenAIBaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{provider: p, config: &Config{Provider: "openai"}}

	tr, err := c.Transcribe(context.Background(), tinyWAV, WithLanguage("en"))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Text != "Hello there." || tr.Duration != 2.5 || len(tr.Segments) != 1 || gotModel != "whisper-1" || gotFile != "audio.wav" {
		t.Fatalf("transcription=%+v model=%q file=%q", tr, gotModel, gotFile)
	}

	sp, err := c.Speak(context.Background(), "Your order shipped.", WithVoice("coral"), WithSpeechInstructions("warm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sp.Data) != "ID3fake-mp3" || sp.MediaType != "audio/mpeg" {
		t.Fatalf("speech = %+v", sp)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(speechBody), &sent)
	if sent["voice"] != "coral" || sent["model"] != "gpt-4o-mini-tts" || sent["instructions"] != "warm" || sent["input"] != "Your order shipped." {
		t.Fatalf("speech request = %v", sent)
	}
}

func TestGeminiSpeakWrapsPCMAsWAV(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"responseModalities":["AUDIO"]`) || !strings.Contains(r.URL.Path, "-tts:generateContent") {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;rate=24000","data":"` + base64.StdEncoding.EncodeToString(pcm) + `"}}]}}]}`))
	}))
	defer srv.Close()
	c := &Client{provider: &geminiProvider{apiKey: "k", model: "gemini-2.0-flash", baseURL: srv.URL}, config: &Config{}}
	sp, err := c.Speak(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if sp.MediaType != "audio/wav" || string(sp.Data[:4]) != "RIFF" || string(sp.Data[8:12]) != "WAVE" || len(sp.Data) != 44+len(pcm) {
		t.Fatalf("wav = %q (%d bytes)", sp.Data[:12], len(sp.Data))
	}
}

func TestGeminiTranscribeSendsAudioInline(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":" Bonjour. "}]},"finishReason":"STOP"}]}`))
	}))
	defer srv.Close()
	c := &Client{provider: &geminiProvider{apiKey: "k", model: "gemini-2.0-flash", baseURL: srv.URL}, config: &Config{}}
	tr, err := c.Transcribe(context.Background(), tinyWAV, WithLanguage("fr"))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Text != "Bonjour." || !strings.Contains(body, `"mimeType":"audio/wav"`) {
		t.Fatalf("text=%q body=%.200s", tr.Text, body)
	}
}

func TestSpeechUnsupportedProvider(t *testing.T) {
	c := NewFake().Client()
	if _, err := c.Speak(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "cannot synthesise") {
		t.Fatalf("err = %v", err)
	}
}
