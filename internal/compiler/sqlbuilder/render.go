package sqlbuilder

import (
	"fmt"
	"strings"
)

// Dialect 方言能力(Renderer 用来调整 SQL 输出)
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
	DialectCH       Dialect = "clickhouse"
	DialectMSSQL    Dialect = "mssql" // SQL Server 2008 R2 兼容模式
)

// Renderer AST → SQL 字符串
// 同时收集参数 ? 对应的实际值
type Renderer struct {
	Dialect Dialect
	// Args 收集的参数(按 ? 出现顺序)
	Args []any
	// identOpen / identClose 标识符引号
	identOpen  string
	identClose string
}

func NewRenderer(d Dialect) *Renderer {
	r := &Renderer{Dialect: d, Args: nil, identOpen: `"`, identClose: `"`}
	switch d {
	case DialectMySQL:
		r.identOpen = "`"
		r.identClose = "`"
	case DialectMSSQL:
		r.identOpen = "["
		r.identClose = "]"
	}
	return r
}

// RenderSelect 渲染 SELECT
func (r *Renderer) RenderSelect(s *SelectStmt) (string, error) {
	var sb strings.Builder

	// SELECT
	sb.WriteString("SELECT ")
	// SQL Server 2008 R2 不支持 LIMIT/OFFSET,用 SELECT TOP N。
	// OFFSET 在 2008 R2 不支持(要 SQL Server 2012+),阉割版不支持分页,只支持 limit。
	if r.Dialect == DialectMSSQL && s.Limit > 0 {
		sb.WriteString(fmt.Sprintf("TOP %d ", s.Limit))
		s.Limit = 0 // 阻止后面再写 LIMIT
	}
	if s.Distinct {
		sb.WriteString("DISTINCT ")
	}
	if len(s.Columns) == 0 {
		return "", fmt.Errorf("select: no columns")
	}
	for i, c := range s.Columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		if err := r.renderExpr(&sb, c.Expr); err != nil {
			return "", err
		}
		if c.Alias != "" {
			sb.WriteString(" AS ")
			sb.WriteString(r.quote(c.Alias))
		}
	}

	// FROM
	if s.From == nil {
		return "", fmt.Errorf("select: missing FROM")
	}
	sb.WriteString(" FROM ")
	if err := r.renderTableRef(&sb, s.From); err != nil {
		return "", err
	}

	// JOIN
	for _, j := range s.Joins {
		sb.WriteString(" ")
		sb.WriteString(j.Type)
		sb.WriteString(" JOIN ")
		if err := r.renderTableRef(&sb, j.Table); err != nil {
			return "", err
		}
		if j.Alias != "" {
			sb.WriteString(" AS ")
			sb.WriteString(r.quote(j.Alias))
		}
		sb.WriteString(" ON ")
		if err := r.renderExpr(&sb, j.On); err != nil {
			return "", err
		}
	}

	// WHERE
	if s.Where != nil {
		sb.WriteString(" WHERE (")
		if err := r.renderExpr(&sb, s.Where); err != nil {
			return "", err
		}
		sb.WriteString(")")
	}

	// GROUP BY
	if len(s.GroupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		for i, g := range s.GroupBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			if err := r.renderExpr(&sb, g); err != nil {
				return "", err
			}
		}
	}

	// ORDER BY
	if len(s.OrderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, o := range s.OrderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			if err := r.renderExpr(&sb, o.Expr); err != nil {
				return "", err
			}
			if o.Desc {
				sb.WriteString(" DESC")
			} else {
				sb.WriteString(" ASC")
			}
		}
	}

	// LIMIT / OFFSET
	// SQL Server 走 SELECT TOP N(在上面已写),这里跳过;其他 dialect 走 LIMIT/OFFSET
	if r.Dialect != DialectMSSQL && s.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", s.Limit))
	}
	if r.Dialect != DialectMSSQL && s.Offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", s.Offset))
	}

	return sb.String(), nil
}

func (r *Renderer) renderTableRef(sb *strings.Builder, t *TableRef) error {
	if t.Subquery != nil {
		sb.WriteString("(")
		sql, err := r.RenderSelect(t.Subquery)
		if err != nil {
			return err
		}
		sb.WriteString(sql)
		sb.WriteString(")")
		if t.Name != "" {
			sb.WriteString(" AS ")
			sb.WriteString(r.quote(t.Name))
		}
		return nil
	}
	name := t.Source
	if name == "" {
		name = t.Name
	}
	sb.WriteString(r.quote(name))
	if t.Name != "" && t.Name != name {
		sb.WriteString(" AS ")
		sb.WriteString(r.quote(t.Name))
	}
	return nil
}

func (r *Renderer) renderExpr(sb *strings.Builder, e Expr) error {
	switch v := e.(type) {
	case *Ident:
		if v.Qualifier != "" {
			sb.WriteString(r.quote(v.Qualifier))
			sb.WriteString(".")
		}
		sb.WriteString(r.quote(v.Name))
	case Ident:
		if v.Qualifier != "" {
			sb.WriteString(r.quote(v.Qualifier))
			sb.WriteString(".")
		}
		sb.WriteString(r.quote(v.Name))
	case *Literal:
		return r.renderLiteral(sb, v.Value)
	case Literal:
		return r.renderLiteral(sb, v.Value)
	case *Param:
		sb.WriteString("?")
		// 参数值由调用方注入
	case Param:
		sb.WriteString("?")
	case *Star:
		sb.WriteString("*")
	case Star:
		sb.WriteString("*")
	case *BinaryOp:
		if err := r.renderExpr(sb, v.L); err != nil {
			return err
		}
		sb.WriteString(" ")
		sb.WriteString(v.Op)
		sb.WriteString(" ")
		if err := r.renderExpr(sb, v.R); err != nil {
			return err
		}
	case *UnaryOp:
		sb.WriteString(v.Op)
		sb.WriteString(" ")
		if err := r.renderExpr(sb, v.X); err != nil {
			return err
		}
	case *Func:
		sb.WriteString(v.Name)
		sb.WriteString("(")
		if v.Distinct {
			sb.WriteString("DISTINCT ")
		}
		for i, a := range v.Args {
			if i > 0 {
				sb.WriteString(", ")
			}
			if err := r.renderExpr(sb, a); err != nil {
				return err
			}
		}
		sb.WriteString(")")
	case *InExpr:
		if err := r.renderExpr(sb, v.Expr); err != nil {
			return err
		}
		if v.Not {
			sb.WriteString(" NOT IN (")
		} else {
			sb.WriteString(" IN (")
		}
		for i, p := range v.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			if err := r.renderExpr(sb, p); err != nil {
				return err
			}
		}
		sb.WriteString(")")
	case *BetweenExpr:
		if err := r.renderExpr(sb, v.Expr); err != nil {
			return err
		}
		if v.Not {
			sb.WriteString(" NOT BETWEEN ")
		} else {
			sb.WriteString(" BETWEEN ")
		}
		if err := r.renderExpr(sb, v.Lo); err != nil {
			return err
		}
		sb.WriteString(" AND ")
		if err := r.renderExpr(sb, v.Hi); err != nil {
			return err
		}
	case *RawExpr:
		// 原始 SQL 片段(由 segment 解析产出,不做引号处理)
		sb.WriteString(v.Raw)
	case *DatePart:
		// SQL Server 的 date part 标识符:DATEADD(<DatePart>, ...)
		// 不加引号(关键!)
		sb.WriteString(v.Unit)
	default:
		return fmt.Errorf("unsupported expr type: %T", e)
	}
	return nil
}

func (r *Renderer) renderLiteral(sb *strings.Builder, v any) error {
	switch x := v.(type) {
	case string:
		sb.WriteString("'")
		sb.WriteString(strings.ReplaceAll(x, "'", "''"))
		sb.WriteString("'")
	case int:
		sb.WriteString(fmt.Sprintf("%d", x))
	case int32:
		sb.WriteString(fmt.Sprintf("%d", x))
	case int64:
		sb.WriteString(fmt.Sprintf("%d", x))
	case float32:
		sb.WriteString(fmt.Sprintf("%v", x))
	case float64:
		sb.WriteString(fmt.Sprintf("%v", x))
	case bool:
		if x {
			sb.WriteString("TRUE")
		} else {
			sb.WriteString("FALSE")
		}
	case nil:
		sb.WriteString("NULL")
	default:
		return fmt.Errorf("unsupported literal type: %T", v)
	}
	return nil
}

func (r *Renderer) quote(name string) string {
	// 如果是 * 之类特殊字符,直接返回
	if name == "*" {
		return "*"
	}
	return r.identOpen + name + r.identClose
}

// Bind 把 ? 参数的实际值注入到 Args
// 返回调整后的 SQL 和参数数组
// 调用方: 先调用 RenderSelect 拿到 SQL,再调用 Bind 拿最终参数
func (r *Renderer) Bind(values []any) []any {
	out := make([]any, 0, len(values))
	out = append(out, values...)
	return out
}
