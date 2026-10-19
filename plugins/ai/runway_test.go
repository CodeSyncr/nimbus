package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunwayAsyncRoutesAndKeepsTextSeparate(t *testing.T) {
	var starts, polls, cancels atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer video-key" || r.Header.Get("X-Runway-Version") != runwayVersion {
			t.Error("missing Runway credentials or version")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/text_to_video", "POST /v1/image_to_video":
			starts.Add(1)
			var b map[string]any
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			if b["model"] != RunwayKlingStandard || b["audio"] != false || b["ratio"] != "720:1280" || b["duration"] != float64(5) {
				t.Errorf("unexpected billable payload: %+v", b)
			}
			if r.URL.Path == "/v1/image_to_video" && b["promptImage"] != "https://example.com/frame.png" {
				t.Error("image was dropped")
			}
			_, _ = w.Write([]byte(`{"id":"task-1","estimatedCost":{"credits":45}}`))
		case "GET /v1/tasks/task-1":
			polls.Add(1)
			_, _ = w.Write([]byte(`{"id":"task-1","status":"SUCCEEDED","output":["https://example.com/film.mp4"]}`))
		case "DELETE /v1/tasks/task-1":
			cancels.Add(1)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected route: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c, err := NewClient(&Config{Provider: "openai", OpenAIKey: "text-key", Model: "text-model", VideoProvider: "runway", VideoAPIKey: "video-key", VideoBaseURL: srv.URL, VideoModel: RunwayKlingStandard})
	if err != nil {
		t.Fatal(err)
	}
	if c.provider.Name() != "openai" || c.config.Model != "text-model" {
		t.Fatal("video settings replaced the text client")
	}
	task, err := Video().WithClient(c).Prompt("A cyclist pedals through warm afternoon light.").Duration(5).Size("720x1280").Start(context.Background())
	if err != nil || task.ID != "task-1" || task.EstimatedCredits != 45 {
		t.Fatalf("start: %+v %v", task, err)
	}
	state, err := c.GetVideoTask(context.Background(), task.ID)
	if err != nil || state.Status != "SUCCEEDED" || len(state.Output) != 1 {
		t.Fatalf("poll: %+v %v", state, err)
	}
	if err = c.CancelVideoTask(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	_, err = Video().WithClient(c).Prompt("Continue the cyclist's movement.").FromImage("https://example.com/frame.png").Size("720x1280").Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 || polls.Load() != 1 || cancels.Load() != 1 {
		t.Fatal("unexpected task lifecycle")
	}
}

func TestRunwayValidationPreventsBillableRequests(t *testing.T) {
	tests := []VideoRequest{{Prompt: "x"}, {Prompt: strings.Repeat("😀", 1251)}, {Prompt: "a scene", Duration: 16}, {Prompt: "a scene", Model: "expensive-unknown"}, {Prompt: "a scene", Size: "1920x1080"}, {Prompt: "a scene", FPS: 24}, {Prompt: "a scene", ImageURL: "http://localhost/image"}}
	for _, req := range tests {
		if _, err := runwayPayload(&req); err == nil {
			t.Errorf("accepted invalid request: %+v", req)
		}
	}
	for _, id := range []string{"", "../task", "a/b", "a?b", "a%2fb"} {
		if _, err := runwayTaskPath(id); err == nil {
			t.Errorf("accepted task path %q", id)
		}
	}
	t.Setenv("RUNWAY_API_KEY", "")
	if _, err := NewClient(&Config{Provider: "runway"}); err == nil {
		t.Fatal("missing key must fail before starting")
	}
}

func TestRunwayNeverRetriesStartAndCancelsInterruptedPoll(t *testing.T) {
	for _, mode := range []string{"start-error", "poll-error", "cancel", "failure", "success"} {
		t.Run(mode, func(t *testing.T) {
			var starts, deletes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "POST":
					starts.Add(1)
					if mode == "start-error" {
						w.WriteHeader(503)
						_, _ = w.Write([]byte("secret-provider-body"))
						return
					}
					_, _ = w.Write([]byte(`{"id":"task-1"}`))
				case "DELETE":
					deletes.Add(1)
					w.WriteHeader(204)
				case "GET":
					if mode == "poll-error" {
						w.WriteHeader(500)
						return
					}
					status := "RUNNING"
					if mode == "failure" {
						status = "FAILED"
					}
					if mode == "success" {
						status = "SUCCEEDED"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-1", "status": status, "failureCode": "SAFETY", "output": []string{"https://example.com/film.mp4"}})
				}
			}))
			defer srv.Close()
			p := &runwayProvider{key: "video-key", baseURL: srv.URL, http: srv.Client(), pollInterval: time.Millisecond}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			result, err := p.GenerateVideo(ctx, &VideoRequest{Prompt: "A cyclist in warm sunlight"})
			if mode == "success" {
				if err != nil || result.TaskID != "task-1" || result.Duration != 5 {
					t.Fatalf("result: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("expected lifecycle error")
			}
			if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-provider-body") {
				t.Fatal("upstream body leaked")
			}
			if starts.Load() != 1 {
				t.Fatalf("billable POST retried: %d", starts.Load())
			}
			wantDelete := int32(0)
			if mode == "cancel" || mode == "poll-error" {
				wantDelete = 1
			}
			if deletes.Load() != wantDelete {
				t.Fatalf("cancellations=%d want=%d", deletes.Load(), wantDelete)
			}
		})
	}
}
