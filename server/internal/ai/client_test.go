package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	status := 200
	response := `{"choices":[{"finish_reason":"stop","message":{"content":"完成"}}]}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("wrong endpoint or authorization")
		}
		var body struct {
			Model    string                           `json:"model"`
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "test-model" || len(body.Messages) != 2 || body.Messages[1].Content != "hello" {
			t.Error("invalid request")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	client, err := New("custom", server.URL+"/v1", "test-model", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client.http = server.Client()
	got, err := client.Generate(context.Background(), "rules", "hello")
	if err != nil || got != "完成" {
		t.Fatalf("got %q: %v", got, err)
	}
	response = `{"choices":[{"finish_reason":"length","message":{"content":"partial"}}]}`
	if _, err = client.Generate(context.Background(), "rules", "hello"); err == nil {
		t.Fatal("accepted truncated response")
	}
	status = 401
	response = "secret"
	if _, err = client.Generate(context.Background(), "rules", "hello"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("error must not expose response")
	}
}

func TestProviderConfiguration(t *testing.T) {
	for _, provider := range []string{"openai", "deepseek", "glm", "minimax"} {
		if _, err := New(provider, "", "model", "key"); err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
	}
	if _, err := New("unknown", "", "model", "key"); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if _, err := New("custom", "http://example.com", "model", "key"); err == nil {
		t.Fatal("insecure endpoint accepted")
	}
}
