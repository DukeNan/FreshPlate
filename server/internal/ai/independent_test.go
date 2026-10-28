package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIndependentProviderRequestsAndFailures(t *testing.T) {
	endpoints := map[string]string{
		"openai":   "https://api.openai.com/v1/chat/completions",
		"deepseek": "https://api.deepseek.com/chat/completions",
		"glm":      "https://open.bigmodel.cn/api/paas/v4/chat/completions",
		"minimax":  "https://api.minimax.io/v1/chat/completions",
	}
	for provider, endpoint := range endpoints {
		t.Run(provider, func(t *testing.T) {
			client, err := New(provider, "", "model", "private-key")
			if err != nil {
				t.Fatal(err)
			}
			client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != endpoint || req.Method != "POST" || req.Header.Get("Authorization") != "Bearer private-key" {
					t.Errorf("incorrect request: %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`)), Header: make(http.Header)}, nil
			})
			if output, err := client.Generate(context.Background(), "rules", "prompt"); err != nil || output != "ok" {
				t.Fatalf("output %q: %v", output, err)
			}
		})
	}
}

func TestIndependentInvalidAIResponses(t *testing.T) {
	client, err := New("openai", "", "model", "private-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"choices":[{"finish_reason":"length","message":{"content":"partial"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"base_resp":{"status_code":1000}}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"` + strings.Repeat("x", 1<<20) + `"}}]}`,
	} {
		client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		if result, err := client.Generate(context.Background(), "", "prompt"); err == nil || result != "" || strings.Contains(err.Error(), "private-key") {
			t.Fatalf("accepted invalid response or leaked key: %q, %v", result, err)
		}
	}
}
