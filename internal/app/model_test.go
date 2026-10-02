package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelDestinationValidation(t *testing.T) {
	for _, endpoint := range []string{"https://example.com", "http://127.0.0.1@evil.test", "file:///tmp/model", "http://127.0.0.1:1234/?secret=x", "http://192.168.1.2:1234", "http://127.0.0.1:1234/other"} {
		if _, err := NewModelClient(ModelSettings{Provider: "ollama", Endpoint: endpoint}, ""); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
}

func TestCLIModelSettingsAndConsent(t *testing.T) {
	for _, provider := range []string{"claude-cli", "codex-cli"} {
		c, err := NewModelClient(ModelSettings{Provider: provider, CLIPath: filepath.Join(t.TempDir(), "missing-cli"), LocalConfirmed: true}, "never-pass-this-token")
		if err != nil {
			t.Fatalf("%s configuration could not be saved: %v", provider, err)
		}
		if _, err := c.Chat(context.Background(), "default", []Message{{Role: "user", Content: "private context"}}); err == nil || !strings.Contains(err.Error(), "cloud") {
			t.Fatalf("%s local confirmation authorized cloud: %v", provider, err)
		}
	}
}
func TestModelOllamaChat(t *testing.T) {
	var payload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"local-test","size":4000}]}`))
		case "/api/show":
			w.Write([]byte(`{"details":{"format":"gguf"},"model_info":{"general.architecture":"test"}}`))
		case "/api/chat":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			w.Write([]byte(`{"message":{"role":"assistant","content":"Check the observed evidence first."},"done":true}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	c, err := NewModelClient(ModelSettings{Provider: "ollama", Endpoint: ts.URL, LocalConfirmed: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := c.Chat(context.Background(), "local-test", []Message{{Role: "system", Content: "Read-only guidance"}, {Role: "user", Content: "Help"}})
	if err != nil || text != "Check the observed evidence first." {
		t.Fatalf("chat %q %v", text, err)
	}
	if payload["stream"] != false || payload["tools"] != nil {
		t.Fatal("unexpected streaming or tools")
	}
}
func TestModelCompatibleAndTools(t *testing.T) {
	tool := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"local"}]}`))
		case "/v1/chat/completions":
			if tool {
				w.Write([]byte(`{"choices":[{"message":{"content":"run","tool_calls":[{"type":"function"}]}}]}`))
			} else {
				w.Write([]byte(`{"choices":[{"message":{"content":"Local advice"}}]}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	c, err := NewModelClient(ModelSettings{Provider: "compatible", Endpoint: ts.URL + "/v1", LocalConfirmed: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if text, err := c.Chat(context.Background(), "local", []Message{{Role: "user", Content: "help"}}); err != nil || text != "Local advice" {
		t.Fatalf("%q %v", text, err)
	}
	tool = true
	if _, err = c.Chat(context.Background(), "local", []Message{{Role: "user", Content: "help"}}); err == nil {
		t.Fatal("accepted tool call")
	}
}

func TestModelCompletionReasons(t *testing.T) {
	for _, tt := range []struct {
		provider, reason, wantError string
	}{
		{"compatible", "", ""},
		{"compatible", "stop", ""},
		{"compatible", "length", "truncated"},
		{"compatible", "content_filter", "did not complete"},
		{"compatible", "tool_calls", "tool action"},
		{"compatible", "function_call", "tool action"},
		{"ollama", "", ""},
		{"ollama", "stop", ""},
		{"ollama", "length", "truncated"},
		{"ollama", "load", "did not complete"},
	} {
		t.Run(tt.provider+"/"+tt.reason, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/models":
					io.WriteString(w, `{"data":[{"id":"local"}]}`)
				case "/api/tags":
					io.WriteString(w, `{"models":[{"name":"local"}]}`)
				case "/api/show":
					io.WriteString(w, `{}`)
				case "/v1/chat/completions", "/api/chat":
					calls.Add(1)
					message := map[string]string{"role": "assistant", "content": "Model reply"}
					response := map[string]any{"message": message, "done": true, "done_reason": tt.reason}
					if tt.provider == "compatible" {
						response = map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": tt.reason}}}
					}
					json.NewEncoder(w).Encode(response)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client, err := NewModelClient(ModelSettings{Provider: tt.provider, Endpoint: server.URL, LocalConfirmed: true}, "")
			if err != nil {
				t.Fatal(err)
			}
			text, err := client.Chat(context.Background(), "local", []Message{{Role: "user", Content: "Help"}})
			if tt.wantError == "" {
				if err != nil || text != "Model reply" {
					t.Fatalf("completed reply = %q, %v", text, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) || text != "" {
				t.Fatalf("incomplete reply = %q, %v; want error containing %q", text, err, tt.wantError)
			}
			if count := calls.Load(); count != 1 {
				t.Fatalf("model was called %d times; want no automatic retry", count)
			}
		})
	}
}

func TestModelRemoteAndUnconfirmed(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[{"name":"remote-cloud","remote_host":"https://cloud.example"}]}`))
			return
		}
		called = true
	}))
	defer ts.Close()
	c, _ := NewModelClient(ModelSettings{Provider: "ollama", Endpoint: ts.URL}, "")
	if _, err := c.Chat(context.Background(), "remote-cloud", []Message{{Role: "user", Content: "private"}}); err == nil {
		t.Fatal("accepted unconfirmed runtime")
	}
	c.settings.LocalConfirmed = true
	if _, err := c.Chat(context.Background(), "remote-cloud", []Message{{Role: "user", Content: "private"}}); err == nil {
		t.Fatal("accepted remote model")
	}
	if called {
		t.Fatal("sent prompt to remote model")
	}
}
func TestModelCancelRedirectAndLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[{"name":"local"}]}`))
			return
		}
		if r.URL.Path == "/api/show" {
			w.Write([]byte(`{}`))
			return
		}
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer ts.Close()
	c, _ := NewModelClient(ModelSettings{Provider: "ollama", Endpoint: ts.URL, LocalConfirmed: true}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Chat(ctx, "local", []Message{{Role: "user", Content: "help"}}); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := c.Chat(context.Background(), "local", []Message{{Role: "user", Content: strings.Repeat("x", 33<<10)}}); err == nil {
		t.Fatal("accepted oversized context")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://192.168.1.1/private", 302) }))
	defer redirect.Close()
	c, _ = NewModelClient(ModelSettings{Provider: "ollama", Endpoint: redirect.URL}, "")
	if _, err := c.Models(context.Background()); err == nil {
		t.Fatal("followed redirect")
	}
}
