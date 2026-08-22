// Package sqlbuilder 自己实现的 SQL AST + 渲染器
// 阉割版设计:
//   - 只支持 SELECT(无 INSERT/UPDATE/DELETE)
//   - 不支持 UNION/CTE
//   - 不支持子查询(W2 阶段,W3 视情况)
//   - 占位符统一 ?(driver-specific 由 Renderer 处理)
package sqlbuilder

// Expr 表达式节点
type Expr interface {
	exprNode()
}

// Ident 标识符(字段、表名、cube 别名)
type Ident struct {
	Qualifier string // 可选:"o" → "o.id"
	Name      string
}

func (Ident) exprNode() {}

// Star * 标识符
type Star struct{}

func (Star) exprNode() {}

// Literal 字面量
type Literal struct {
	Value any // string / int64 / float64 / bool / nil
}

func (Literal) exprNode() {}

// Param 参数占位符
type Param struct {
	N int // 第几个参数,从 1 开始
}

func (Param) exprNode() {}

// BinaryOp 二元操作
// Op: =, <>, !=, <, <=, >, >=, AND, OR, LIKE, NOT LIKE, IN, NOT IN
type BinaryOp struct {
	Op       string
	L, R     Expr
	Negate   bool // NOT IN / NOT LIKE
}

func (*BinaryOp) exprNode() {}

// UnaryOp 一元操作
// Op: NOT, IS NULL, IS NOT NULL, EXISTS
type UnaryOp struct {
	Op string
	X  Expr
}

func (*UnaryOp) exprNode() {}

// Func 函数调用(COUNT/SUM/AVG/COALESCE/DATE_TRUNC 等)
type Func struct {
	Name string
	Args []Expr
	// Distinct 用于 COUNT(DISTINCT col)
	Distinct bool
}

func (*Func) exprNode() {}

// Cast 类型转换
// 阉割版暂不实现,留接口
type Cast struct {
	Expr Expr
	Type string
}

func (*Cast) exprNode() {}

// DatePart SQL Server / 部分方言的 date part 标识符
// 例:DATEADD(<DatePart>, ...) 中的第一参数应该是 dd/mm/yy 等标识符,不是字符串
// 跟 Ident 区别:DatePart 不带 qualifier,且渲染时不加引号
type DatePart struct {
	Unit string // "dd" / "mm" / "yy" / "wk" / "qq" 等
}

func (*DatePart) exprNode() {}

// InExpr IN 表达式(值列表)
type InExpr struct {
	Expr  Expr
	List  []Expr // 多个 ? 占位
	Not   bool
}

func (*InExpr) exprNode() {}

// BetweenExpr BETWEEN
type BetweenExpr struct {
	Expr Expr
	Lo, Hi Expr
	Not   bool
}

func (*BetweenExpr) exprNode() {}

// ============================================================
// 表达式构造便利函数
// ============================================================

func Col(name string) Expr                   { return Ident{Name: name} }
func QCol(qualifier, name string) Expr       { return Ident{Qualifier: qualifier, Name: name} }
func Lit(v any) Expr                         { return Literal{Value: v} }
func P(n int) Expr                           { return Param{N: n} }
func Star_() Expr                            { return Star{} }
func DatePart_(unit string) Expr             { return &DatePart{Unit: unit} }

func Eq(a, b Expr) Expr                      { return &BinaryOp{Op: "=", L: a, R: b} }
func Ne(a, b Expr) Expr                      { return &BinaryOp{Op: "<>", L: a, R: b} }
func Lt(a, b Expr) Expr                      { return &BinaryOp{Op: "<", L: a, R: b} }
func Le(a, b Expr) Expr                      { return &BinaryOp{Op: "<=", L: a, R: b} }
func Gt(a, b Expr) Expr                      { return &BinaryOp{Op: ">", L: a, R: b} }
func Ge(a, b Expr) Expr                      { return &BinaryOp{Op: ">=", L: a, R: b} }
func And(a, b Expr) Expr                     { return &BinaryOp{Op: "AND", L: a, R: b} }
func Or(a, b Expr) Expr                      { return &BinaryOp{Op: "OR", L: a, R: b} }
func Like(a, b Expr) Expr                    { return &BinaryOp{Op: "LIKE", L: a, R: b} }
func NotIn(a Expr, list []Expr) Expr         { return &InExpr{Expr: a, List: list, Not: true} }
func In(a Expr, list []Expr) Expr            { return &InExpr{Expr: a, List: list} }

func IsNull(a Expr) Expr                     { return &UnaryOp{Op: "IS NULL", X: a} }
func IsNotNull(a Expr) Expr                  { return &UnaryOp{Op: "IS NOT NULL", X: a} }
func Not_(a Expr) Expr                       { return &UnaryOp{Op: "NOT", X: a} }

func Count(a Expr) Expr                      { return &Func{Name: "COUNT", Args: []Expr{a}} }
func CountStar() Expr                        { return &Func{Name: "COUNT", Args: []Expr{Star_()}, Distinct: false} }
func CountDistinct(a Expr) Expr              { return &Func{Name: "COUNT", Args: []Expr{a}, Distinct: true} }
func Sum(a Expr) Expr                        { return &Func{Name: "SUM", Args: []Expr{a}} }
func Avg(a Expr) Expr                        { return &Func{Name: "AVG", Args: []Expr{a}} }
func Min(a Expr) Expr                        { return &Func{Name: "MIN", Args: []Expr{a}} }
func Max(a Expr) Expr                        { return &Func{Name: "MAX", Args: []Expr{a}} }
func Coalesce(args ...Expr) Expr             { return &Func{Name: "COALESCE", Args: args} }

// RTRIM SQL Server / 标准 SQL 字符串修剪函数
//   解决 CHAR 字段的尾随空格(让 equals filter 直接命中)
func RTRIM(a Expr) Expr                      { return &Func{Name: "RTRIM", Args: []Expr{a}} }

// LTRIM 字符串左修剪
func LTRIM(a Expr) Expr                      { return &Func{Name: "LTRIM", Args: []Expr{a}} }

// TRIM 字符串两端修剪
func TRIM(a Expr) Expr                      { return &Func{Name: "TRIM", Args: []Expr{a}} }

// IsNullOrEmpty 兼容 NULL 或空字符串(a IS NULL OR a = '')
func IsNullOrEmpty(a Expr) Expr {
	return Or(IsNull(a), Eq(a, Lit("")))
}

// DateTrunc 时间截断(SQL 标准: DATE_TRUNC('day', col))
// 实际 SQL 方言: PG/CH 用 DATE_TRUNC,MySQL 用 DATE_FORMAT,SQLite 用 strftime
// W2 阶段先支持 PG/CH/SQLite 的 DATE_TRUNC,方言差异在 Renderer 处理
func DateTrunc(granularity string, a Expr) Expr {
	return &Func{Name: "DATE_TRUNC", Args: []Expr{Lit(granularity), a}}
}
