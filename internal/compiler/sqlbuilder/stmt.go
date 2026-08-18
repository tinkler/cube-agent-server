package sqlbuilder

// SelectStmt SELECT 语句
// 阉割版: 单 FROM,可选 JOIN,WHERE,GROUP BY,ORDER BY,LIMIT,OFFSET
type SelectStmt struct {
	Columns   []SelectColumn
	From      *TableRef
	Joins     []JoinClause
	Where     Expr
	GroupBy   []Expr
	OrderBy   []OrderBy
	Limit     int
	Offset    int
	Distinct  bool
}

// SelectColumn SELECT 列表项
type SelectColumn struct {
	Expr  Expr
	Alias string // 输出列名
}

// TableRef 表引用
type TableRef struct {
	Name     string     // 表名或别名
	Source   string     // 实际物理表名(可能与 Name 不同,用于 alias 场景)
	Subquery *SelectStmt // W3 才用,W2 不支持子查询
}

// JoinClause JOIN 子句
type JoinClause struct {
	Type  string // "INNER" / "LEFT"
	Table *TableRef
	On    Expr
	Alias string
}

// OrderBy 排序
type OrderBy struct {
	Expr Expr
	Desc bool
}
