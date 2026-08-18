// Package llm LLM 客户端抽象
// 当前实现:DeepSeek(兼容 OpenAI 协议)
package llm

import (
	"context"
	"errors"
	"fmt"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// Config LLM 配置
type Config struct {
	Provider   string        // "deepseek" | "openai" | "custom"
	APIKey     string
	BaseURL    string        // DeepSeek: https://api.deepseek.com/v1
	Model      string        // DeepSeek: deepseek-chat
	Timeout    time.Duration
	MaxRetries int
}

// Client LLM 客户端接口
type Client interface {
	// Chat 简单对话(单轮 prompt → 文本响应)
	Chat(ctx context.Context, system, user string) (string, error)
	// ChatJSON 期望 LLM 返回 JSON(用于结构化输出)
	ChatJSON(ctx context.Context, system, user string, out any) error
	// Close 关闭
	Close() error
}

// NewClient 根据 Config 构造 Client
func NewClient(cfg Config) (Client, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("llm: APIKey is required")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("llm: BaseURL is required")
	}
	if cfg.Model == "" {
		return nil, errors.New("llm: Model is required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 180 * time.Second
	}
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	oc := openai.DefaultConfig(cfg.APIKey)
	oc.BaseURL = cfg.BaseURL

	return &openaiClient{
		oc:         oc,
		model:      cfg.Model,
		timeout:    timeout,
		maxRetries: maxRetries,
	}, nil
}

// openaiClient 通用 OpenAI 协议客户端
type openaiClient struct {
	oc         openai.ClientConfig
	model      string
	timeout    time.Duration
	maxRetries int
}

func (c *openaiClient) Close() error {
	return nil
}

func (c *openaiClient) Chat(ctx context.Context, system, user string) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: user},
		},
		// 较低温度,期望稳定输出
		Temperature: 0.2,
	}
	resp, err := c.sendWithRetry(ctx, req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("llm: empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

func (c *openaiClient) ChatJSON(ctx context.Context, system, user string, out any) error {
	// 强化 prompt:返回 JSON
	sys := system + "\n\n【重要】你的回答必须是纯 JSON,不要包含 markdown 代码块标记。"

	raw, err := c.Chat(ctx, sys, user)
	if err != nil {
		return err
	}
	// 兜底:如果 LLM 还是带 markdown 标记,剥掉
	raw = stripMarkdownCodeFence(raw)
	return unmarshalJSON(raw, out)
}

func (c *openaiClient) sendWithRetry(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
	client := openai.NewClientWithConfig(c.oc)
	var lastErr error
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
		resp, err := client.CreateChatCompletion(timeoutCtx, req)
		cancel()
		if err == nil {
			return &resp, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, fmt.Errorf("llm: %w", err)
		}
		// 简单 backoff: 1s, 2s, 4s
		delay := time.Duration(1<<attempt) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, fmt.Errorf("llm: max retries exceeded: %w", lastErr)
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// 简单的判定:网络错误 / 5xx / context 超时 可重试;4xx 不重试
	for _, prefix := range []string{"connection", "timeout", "deadline", "EOF", "reset", "502", "503", "504"} {
		if contains(s, prefix) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func stripMarkdownCodeFence(s string) string {
	// 处理 ```json ... ``` 包裹
	const (
		start = "```"
	)
	if len(s) >= len(start) && s[:len(start)] == start {
		// 跳到第一个换行
		i := indexOf(s, "\n")
		if i < 0 {
			return s
		}
		s = s[i+1:]
		// 去掉结尾 ```
		j := indexOf(s, "```")
		if j >= 0 {
			s = s[:j]
		}
	}
	return s
}
