// Package ai provides a text generation boundary for future recipe assistance.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Generator is independent of the external provider and must not be used for safety decisions.
type Generator interface {
	Generate(ctx context.Context, system, prompt string) (string, error)
}

type Client struct {
	endpoint, key, model string
	http                 *http.Client
}

// New creates a text-only client; model and key are supplied by the operator.
// baseURL may be supplied for a compatible provider or a regional endpoint.
func New(provider, baseURL, model, key string) (*Client, error) {
	defaults := map[string]string{"openai": "https://api.openai.com/v1", "deepseek": "https://api.deepseek.com", "glm": "https://open.bigmodel.cn/api/paas/v4", "minimax": "https://api.minimax.io/v1"}
	if baseURL == "" {
		baseURL = defaults[provider]
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(model) == "" || strings.TrimSpace(key) == "" {
		return nil, errors.New("invalid AI provider configuration")
	}
	return &Client{endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions", model: model, key: key, http: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Generate(ctx context.Context, system, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty AI prompt")
	}
	messages := []map[string]string{}
	if system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})
	body, err := json.Marshal(struct {
		Model    string              `json:"model"`
		Messages []map[string]string `json:"messages"`
	}{c.model, messages})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("invalid AI request")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", errors.New("AI provider request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI provider returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		BaseResp struct {
			StatusCode int `json:"status_code"`
		} `json:"base_resp"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil || result.BaseResp.StatusCode != 0 || len(result.Choices) == 0 || result.Choices[0].FinishReason != "stop" || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("invalid or incomplete AI response")
	}
	return result.Choices[0].Message.Content, nil
}
