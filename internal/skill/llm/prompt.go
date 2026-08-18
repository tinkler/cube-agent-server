package llm

// Prompt 模板集合
// 所有 prompt 都是 system + user 两段,user 是变量

// AnalyzeDatasourcePromptStep1 系统提示
// 任务: 从表名/字段名/外键推断业务含义
const AnalyzeDatasourceSystemPrompt = `你是数据建模专家,擅长从数据库 schema 推断业务含义。
你的输出必须严格遵循 JSON 格式,字段如下:
{
  "tables": [
    {
      "name": "原表名",
      "type": "fact|dimension|bridge",
      "description": "中文业务描述(1-2 句)",
      "reasoning": "推断理由(关键,2-3 句说明你是怎么判断的)",
      "primary_key": "主键字段名(没有填 null)",
      "foreign_keys": [
        {"column": "外键字段", "references": "目标表.目标字段", "confidence": 0.0-1.0}
      ]
    }
  ]
}

判断规则:
- type=fact:有 created_at/updated_at + 多个外键 + 命名像 orders/events/logs/transactions
- type=dimension:命名像 users/products/categories/countries + 主键是 id + 没有大量外键
- type=bridge:多对多关系表,命名像 user_roles/order_items
- 字段命名约定:id=主键,_id=外键,no/num=业务键
- confidence 0.0-1.0 表示你对外键推断的确信度,1.0=外键约束已声明
- 不确定的字段填 null,不要瞎猜`

// BuildAnalyzeDatasourceUserPrompt 构造 user prompt
//   限制:每张表只列前 maxColsPerTable 个字段(避免 LLM 输出 JSON 截断)
//        最多 maxTables 张表(从前面取,通常 introspect 已经过滤过)
func BuildAnalyzeDatasourceUserPrompt(dsName string, tables []TableForAnalysis) string {
	const (
		maxTables       = 30  // 229 张表时,LLM 一次性看不完,只取前 30
		maxColsPerTable = 8   // 每张表前 8 个字段足够 LLM 推断表用途
	)
	sliced := tables
	if len(sliced) > maxTables {
		sliced = sliced[:maxTables]
	}
	// 简化:用紧凑 JSON 输入
	s := "# 数据源: " + dsName + "\n\n## 表清单(" + itoa(len(sliced))
	if len(tables) > maxTables {
		s += " / 共 " + itoa(len(tables))
	}
	s += ")\n"
	for _, t := range sliced {
		s += "### " + t.Name + "\n"
		s += "字段(前 " + itoa(min(len(t.Columns), maxColsPerTable)) + " 个):\n"
		for i, c := range t.Columns {
			if i >= maxColsPerTable {
				s += "- ...(省略 " + itoa(len(t.Columns)-maxColsPerTable) + " 个字段)\n"
				break
			}
			s += "- " + c.Name + " : " + c.Type + (nullableSuffix(c.Nullable))
			if c.Comment != "" {
				s += " -- " + c.Comment
			}
			s += "\n"
		}
		if len(t.ForeignKeys) > 0 {
			s += "外键:\n"
			for _, fk := range t.ForeignKeys {
				s += "- " + fk.Column + " → " + fk.References + "\n"
			}
		}
		s += "\n"
	}
	s += "请分析这些表的业务含义、类型、关系。你的回答必须是纯 JSON。"
	return s
}

func itoa(n int) string {
	// 避免 fmt import
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func nullableSuffix(nullable bool) string {
	if nullable {
		return " (nullable)"
	}
	return ""
}

// TableForAnalysis 喂给 LLM 的表摘要
type TableForAnalysis struct {
	Name        string
	Columns     []ColumnForAnalysis
	ForeignKeys []FKForAnalysis
}

// ColumnForAnalysis 字段
type ColumnForAnalysis struct {
	Name     string
	Type     string
	Nullable bool
	Comment  string
}

// FKForAnalysis 外键
type FKForAnalysis struct {
	Column     string
	References string
}

// ============================================================
// Step 4: 引导用户设计 cube
// ============================================================

// DesignCubeSystemPrompt step4 系统提示
const DesignCubeSystemPrompt = `你是 cube.js 数据建模助手。
给定一组可用表/字段,基于用户的业务需求,推荐一个 cube 定义。
cube.js cube 的 JSON 结构如下:
{
  "name": "cube 英文名(小写+下划线)",
  "sql": "完整 SQL,如 SELECT * FROM xxx WHERE tenant_id = '${SECURITY.tenant_id}'",
  "description": "业务描述(中文,1-2 句)",
  "primary_key": "主键字段",
  "measures": [
    {"name": "count", "type": "count"},
    {"name": "xxx_sum", "type": "sum", "sql": "字段名"},
    {"name": "xxx_avg", "type": "avg", "sql": "字段名"}
  ],
  "dimensions": [
    {"name": "字段名", "sql": "原始字段名", "type": "string|number|time|boolean"}
  ],
  "segments": [
    {"name": "xxx", "sql": "{CUBE}.status IN (...)"}
  ]
}

要求:
1. 至少 1 个 measure,至少 1 个 dimension
2. 所有引用的字段必须存在于提供的表里
3. security 过滤占位符用 ${SECURITY.x} 形式
4. 如果表有时间字段(created_at/updated_at/date),加入一个 time dimension
5. sql 字段用 RAW SQL 字符串(不要 JSON-escape)
6. 你必须返回纯 JSON,不要 markdown`

// BuildDesignCubeUserPrompt step4 user prompt
func BuildDesignCubeUserPrompt(intent string, tables []TableForAnalysis) string {
	s := "# 业务需求\n" + intent + "\n\n# 可用表\n"
	for _, t := range tables {
		s += "## " + t.Name + "\n"
		for _, c := range t.Columns {
			s += "- " + c.Name + " : " + c.Type + "\n"
		}
		s += "\n"
	}
	s += "请基于以上需求,推荐一个 cube 定义(纯 JSON 格式)。"
	return s
}
