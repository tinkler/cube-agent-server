// Package analyzer LLM 分析层
// 从 introspect 拿到的 Meta → 调 LLM 补全业务含义/类型/关系
package analyzer

import (
	"context"
	"fmt"
	"sort"

	"github.com/tinkler/cube-agent-server/internal/skill/datasource"
	"github.com/tinkler/cube-agent-server/internal/skill/llm"
)

// LLMAnalysis LLM 返回的结构化分析
type LLMAnalysis struct {
	Tables []LLMTableAnalysis `json:"tables"`
}

type LLMTableAnalysis struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"` // fact/dimension/bridge
	Description string  `json:"description"`
	Reasoning   string  `json:"reasoning"`
	PrimaryKey  string  `json:"primary_key"`
	ForeignKeys []struct {
		Column     string  `json:"column"`
		References string  `json:"references"`
		Confidence float64 `json:"confidence"`
	} `json:"foreign_keys"`
}

// Analyzer LLM 分析器
type Analyzer struct {
	llm llm.Client
}

// New 构造
func New(c llm.Client) *Analyzer {
	return &Analyzer{llm: c}
}

// Analyze 用 LLM 补全 Meta 的业务含义
// 写回 type/description/reasoning/inferred_join_paths
// intent: 用户的业务诉求(中文),用来排序表,把可能相关的业务表放前面
func (a *Analyzer) Analyze(ctx context.Context, meta *datasource.Meta, intent string) error {
	if a.llm == nil {
		// 没有 LLM,跳过
		return nil
	}

	// 准备 LLM 输入
	tables := make([]llm.TableForAnalysis, 0, len(meta.Tables))
	for _, t := range meta.Tables {
		cols := make([]llm.ColumnForAnalysis, 0, len(t.Columns))
		for _, c := range t.Columns {
			cols = append(cols, llm.ColumnForAnalysis{
				Name:     c.Name,
				Type:     c.Type,
				Nullable: c.Nullable,
				Comment:  c.Comment,
			})
		}
		fks := make([]llm.FKForAnalysis, 0, len(t.ForeignKeys))
		for _, fk := range t.ForeignKeys {
			fks = append(fks, llm.FKForAnalysis{
				Column:     fk.Column,
				References: fk.References,
			})
		}
		tables = append(tables, llm.TableForAnalysis{
			Name:        t.Name,
			Columns:     cols,
			ForeignKeys: fks,
		})
	}

	// 按 intent 关键词重排:命中关键词的表排前面
	tables = rankTablesByIntent(tables, intent)

	prompt := llm.BuildAnalyzeDatasourceUserPrompt(meta.Datasource, tables)
	var result LLMAnalysis
	if err := a.llm.ChatJSON(ctx, llm.AnalyzeDatasourceSystemPrompt, prompt, &result); err != nil {
		return fmt.Errorf("analyzer: llm call: %w", err)
	}

	// 合并回 meta
	for _, t := range result.Tables {
		for i := range meta.Tables {
			if meta.Tables[i].Name == t.Name {
				meta.Tables[i].Type = t.Type
				meta.Tables[i].Description = t.Description
				meta.Tables[i].Reasoning = t.Reasoning
				if meta.Tables[i].PrimaryKey == "" && t.PrimaryKey != "" {
					meta.Tables[i].PrimaryKey = t.PrimaryKey
				}
				// 合并推断的外键
				for _, fk := range t.ForeignKeys {
					meta.Tables[i].ForeignKeys = append(meta.Tables[i].ForeignKeys, datasource.ForeignKeyMeta{
						Column:     fk.Column,
						References: fk.References,
						Confidence: fk.Confidence,
					})
				}
				break
			}
		}
	}
	return nil
}

// rankTablesByIntent 按用户 intent 的关键词重排表。
// 命中关键词的表排前面,没命中的按原顺序在后面。
// intent 是中文自然语言(比如"商品主表"、"库存"、"销售订单")。
func rankTablesByIntent(tables []llm.TableForAnalysis, intent string) []llm.TableForAnalysis {
	if intent == "" || len(tables) == 0 {
		return tables
	}
	// 从 intent 提取关键词(简单按空格/标点切,中文 2-gram 也算)
	keywords := extractKeywords(intent)
	if len(keywords) == 0 {
		return tables
	}
	// 算每张表的命中分数
	type scored struct {
		t llm.TableForAnalysis
		s int
	}
	scoredTables := make([]scored, len(tables))
	for i, t := range tables {
		score := 0
		name := t.Name
		for _, kw := range keywords {
			// 表名包含关键词(中文子串,任意位置)
			if contains(name, kw) {
				score += 10
			}
			// 字段名包含关键词
			for _, c := range t.Columns {
				if contains(c.Name, kw) {
					score += 2
				}
			}
		}
		scoredTables[i] = scored{t, score}
	}
	// 稳定排序:分数高在前
	// 用 sort.SliceStable
	sort.SliceStable(scoredTables, func(i, j int) bool {
		return scoredTables[i].s > scoredTables[j].s
	})
	out := make([]llm.TableForAnalysis, len(tables))
	for i, s := range scoredTables {
		out[i] = s.t
	}
	return out
}

// extractKeywords 从中文 intent 提取关键词(简单分词:每个 2-4 字连续子串,过滤常见停用词)
func extractKeywords(intent string) []string {
	// 去除标点 + 切词
	// 简化:取所有 2-4 字的连续子串(去重),作为候选
	// 实际够用,因为 intent 通常很短
	runes := []rune(intent)
	seen := map[string]bool{}
	out := []string{}
	// 跳过太短的(1 字) 和 太长的(>4 字)
	for i := 0; i < len(runes); i++ {
		for l := 2; l <= 4 && i+l <= len(runes); l++ {
			s := string(runes[i : i+l])
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// contains unicode 安全的子串包含
func contains(s, sub string) bool {
	// bytes.Contains 不支持中文,改 rune 循环
	sr := []rune(s)
	subr := []rune(sub)
	if len(subr) == 0 {
		return true
	}
	if len(subr) > len(sr) {
		return false
	}
	for i := 0; i+len(subr) <= len(sr); i++ {
		match := true
		for j := 0; j < len(subr); j++ {
			if sr[i+j] != subr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
