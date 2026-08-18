package analyzer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkler/cube-agent-server/internal/skill/datasource"
)

// mockClient 测试用 mock LLM
type mockClient struct {
	resp string
	err  error
	// calls 记录
	systemCalls []string
	userCalls   []string
}

func (m *mockClient) Chat(ctx context.Context, system, user string) (string, error) {
	m.systemCalls = append(m.systemCalls, system)
	m.userCalls = append(m.userCalls, user)
	return m.resp, m.err
}

func (m *mockClient) ChatJSON(ctx context.Context, system, user string, out any) error {
	m.systemCalls = append(m.systemCalls, system)
	m.userCalls = append(m.userCalls, user)
	if m.err != nil {
		return m.err
	}
	// 直接调 Chat
	_, err := m.Chat(ctx, system, user)
	if err != nil {
		return err
	}
	// 简单 stub:不真解析
	return nil
}

func (m *mockClient) Close() error { return nil }

func TestAnalyzer_NoLLM(t *testing.T) {
	a := New(nil)
	meta := &datasource.Meta{Datasource: "test"}
	err := a.Analyze(context.Background(), meta, "")
	assert.NoError(t, err)
}

func TestAnalyzer_LLMError(t *testing.T) {
	mc := &mockClient{err: errors.New("boom")}
	a := New(mc)
	meta := &datasource.Meta{
		Datasource: "test",
		Tables: []datasource.TableMeta{
			{Name: "orders", Columns: []datasource.ColumnMeta{{Name: "id", Type: "int"}}},
		},
	}
	err := a.Analyze(context.Background(), meta, "")
	assert.Error(t, err)
}

func TestAnalyzer_Success(t *testing.T) {
	mc := &mockClient{
		resp: `{"tables":[{"name":"orders","type":"fact","description":"订单主表","reasoning":"有 created_at + 外键","primary_key":"id","foreign_keys":[]}]}`,
	}
	a := New(mc)
	meta := &datasource.Meta{
		Datasource: "test",
		Tables: []datasource.TableMeta{
			{Name: "orders", Columns: []datasource.ColumnMeta{{Name: "id", Type: "int"}}},
		},
	}
	err := a.Analyze(context.Background(), meta, "")
	require.NoError(t, err)
	// 验证 systemCalls / userCalls 至少 1 次
	assert.NotEmpty(t, mc.systemCalls)
	assert.NotEmpty(t, mc.userCalls)
	// 验证 user prompt 包含表名
	assert.Contains(t, mc.userCalls[0], "orders")
}
