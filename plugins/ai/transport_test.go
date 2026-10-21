package ai

import (
	"net/http"
	"testing"
)

type stubTransport struct{}

func (stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrHandlerTimeout
}

// A replaced http.DefaultTransport (a tracing wrapper, a test fake) used to
// panic the OpenAI and Anthropic constructors on a *http.Transport cast.
func TestProvidersAcceptReplacedDefaultTransport(t *testing.T) {
	prev := http.DefaultTransport
	http.DefaultTransport = stubTransport{}
	t.Cleanup(func() { http.DefaultTransport = prev })

	p, err := newOpenAIProvider(&Config{OpenAIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.httpClient.Transport.(stubTransport); !ok {
		t.Fatalf("openai transport = %T, want the replaced DefaultTransport", p.httpClient.Transport)
	}
	whole, stream := anthropicClients(&Config{Timeout: 5})
	if _, ok := whole.Transport.(stubTransport); !ok {
		t.Fatalf("anthropic transport = %T, want the replaced DefaultTransport", whole.Transport)
	}
	if stream.Transport != whole.Transport {
		t.Fatal("anthropic clients no longer share one transport")
	}
}

func TestOwnTransportCopiesAndTunesDefault(t *testing.T) {
	tr, ok := ownTransport(func(t *http.Transport) { t.MaxIdleConns = 7 }).(*http.Transport)
	if !ok || tr == http.DefaultTransport {
		t.Fatalf("ownTransport = %T, want a copy of DefaultTransport", tr)
	}
	if tr.MaxIdleConns != 7 {
		t.Fatal("tune not applied")
	}
}
