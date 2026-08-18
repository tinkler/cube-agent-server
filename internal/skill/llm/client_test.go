package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripMarkdownCodeFence(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no fence",
			input:    `{"a":1}`,
			expected: `{"a":1}`,
		},
		{
			name:     "json fence",
			input:    "```json\n{\"a\":1}\n```",
			expected: "{\"a\":1}\n",
		},
		{
			name:     "plain fence",
			input:    "```\n{\"a\":1}\n```",
			expected: "{\"a\":1}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripMarkdownCodeFence(c.input)
			assert.Equal(t, c.expected, got)
		})
	}
}

func TestNewClient_MissingKey(t *testing.T) {
	_, err := NewClient(Config{
		Provider: "deepseek",
		BaseURL:  "https://api.deepseek.com/v1",
		Model:    "deepseek-chat",
	})
	assert.Error(t, err)
}

func TestBuildAnalyzeDatasourceUserPrompt(t *testing.T) {
	tables := []TableForAnalysis{
		{
			Name: "orders",
			Columns: []ColumnForAnalysis{
				{Name: "id", Type: "int"},
				{Name: "amount", Type: "numeric"},
				{Name: "status", Type: "varchar"},
			},
		},
	}
	prompt := BuildAnalyzeDatasourceUserPrompt("test", tables)
	assert.Contains(t, prompt, "orders")
	assert.Contains(t, prompt, "amount")
	assert.Contains(t, prompt, "numeric")
}

func TestBuildDesignCubeUserPrompt(t *testing.T) {
	tables := []TableForAnalysis{
		{Name: "orders", Columns: []ColumnForAnalysis{{Name: "id", Type: "int"}}},
	}
	prompt := BuildDesignCubeUserPrompt("统计每日订单", tables)
	assert.Contains(t, prompt, "统计每日订单")
	assert.Contains(t, prompt, "orders")
}
