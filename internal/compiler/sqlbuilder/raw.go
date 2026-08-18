package sqlbuilder

// RawExpr 原始 SQL 片段
// 用于:
//   - segment 解析(SQL 是用户自定义的,不能走完整 AST)
//   - 一些方言特有的语法(SQL Server 的 OUTPUT,CH 的 SAMPLE 等)
//
// ⚠️ 不要从用户输入直接构造,会引入 SQL 注入风险
//   segment 的 SQL 来自 plugin.yaml(受信),可以安全使用
type RawExpr struct {
	Raw string
}

func (*RawExpr) exprNode() {}

// Raw 构造 RawExpr
func Raw(s string) Expr { return &RawExpr{Raw: s} }
