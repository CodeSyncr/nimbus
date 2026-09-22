package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIGeneratesImages(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer srv.Close()
	p, err := newOpenAIProvider(&Config{OpenAIKey: "k", OpenAIBaseURL: srv.URL + "/v1", ImageModel: "agnes-image-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	var ip ImageProvider = p
	res, err := ip.GenerateImage(context.Background(), &ImageRequest{Prompt: "a megaphone", Size: "1024x1536"})
	if err != nil {
		t.Fatal(err)
	}
	if got["model"] != "agnes-image-2.5-flash" || got["response_format"] != "b64_json" || got["size"] != "1024x1536" {
		t.Errorf("request = %v", got)
	}
	if len(res.Images) != 1 || res.Images[0].B64JSON != "aGVsbG8=" {
		t.Errorf("response = %+v", res)
	}
}
