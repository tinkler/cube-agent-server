package compiler

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/security"
)

// Pass3Result Pass3 输出
type Pass3Result struct {
	SQL  string
	Args []any
}

// Pass3 从 ResolvedIR 生成 SQL
// 阉割版: 单 cube,无 join
func Pass3(ir *ResolvedIR, dialect sqlbuilder.Dialect, sec *security.Context) (*Pass3Result, error) {
	cube := ir.Cube
	if cube == nil {
		return nil, fmt.Errorf("compiler: no cube in resolved IR")
	}

	// 1. 构造 SELECT columns
	cols := []sqlbuilder.SelectColumn{}
	for _, m := range ir.Measures {
		cols = append(cols, sqlbuilder.SelectColumn{
			Expr:  m.Expr,
			Alias: m.Name, // "orders.count" → 直接作为输出列名
		})
	}
	for _, d := range ir.Dimensions {
		cols = append(cols, sqlbuilder.SelectColumn{
			Expr:  d.Expr,
			Alias: d.Name,
		})
	}
	for _, td := range ir.TimeDimensions {
		cols = append(cols, sqlbuilder.SelectColumn{
			Expr:  rewriteTimeExprForDialect(td.Expr, dialect, td.Granularity),
			Alias: td.Name,
		})
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("compiler: no columns in resolved IR")
	}

	// 2. 构造 FROM:从 cube.SQL 提取物理表(简化:作为子查询,别名为 cube name)
	//   阉割版: cube.SQL 整段作为 derived table,alias = cube name
	fromSQL := cube.SQL
	// 注入 security 占位符
	fromSQL = renderSecurity(fromSQL, sec)

	from := &sqlbuilder.TableRef{
		Name:   cube.Name,
		Source: cube.Name,
	}

	// 3. 构造 WHERE:filters + segments + timeDimensions dateRange
	var where sqlbuilder.Expr
	var whereParts []sqlbuilder.Expr

	// segments
	for _, segRef := range ir.Segments {
		_, segName, err := splitCubeField(segRef)
		if err != nil {
			return nil, fmt.Errorf("invalid segment reference %q: %w", segRef, err)
		}
		for i := range cube.Segments {
			if cube.Segments[i].Name == segName {
				whereParts = append(whereParts, parseSegment(cube.Segments[i].SQL, cube.Name))
				break
			}
		}
	}

	// filters
	for _, f := range ir.Filters {
		whereParts = append(whereParts, f.Expr)
	}

	// timeDimensions dateRange
	for _, td := range ir.TimeDimensions {
		if len(td.DateRange) == 2 {
			col := sqlbuilder.Col(fieldSQL(td.Def.SQL, td.Def.Name))
			whereParts = append(whereParts, sqlbuilder.And(
				sqlbuilder.Ge(col, sqlbuilder.Lit(td.DateRange[0])),
				sqlbuilder.Le(col, sqlbuilder.Lit(td.DateRange[1])),
			))
		}
	}

	if len(whereParts) > 0 {
		where = whereParts[0]
		for i := 1; i < len(whereParts); i++ {
			where = sqlbuilder.And(where, whereParts[i])
		}
	}

	// 4. 构造 GROUP BY:所有 dimension + timeDimension
	groupBy := []sqlbuilder.Expr{}
	for _, d := range ir.Dimensions {
		groupBy = append(groupBy, d.Expr)
	}
	for _, td := range ir.TimeDimensions {
		groupBy = append(groupBy, rewriteTimeExprForDialect(td.Expr, dialect, td.Granularity))
	}

	// 5. 构造 SELECT stmt
	//    ORDER BY 也需要对方言重写(time dim 的 DateTrunc 在 MSSQL 必须改成 DATEADD/DATEDIFF)
	orderBy := rewriteOrderByForDialect(ir.OrderBy, ir.TimeDimensions, dialect)
	stmt := &sqlbuilder.SelectStmt{
		Columns:  cols,
		From:     from,
		Where:    where,
		GroupBy:  groupBy,
		OrderBy:  orderBy,
		Limit:    ir.Limit,
		Offset:   ir.Offset,
	}

	// 6. W5: 多 cube 渲染 join
	joins := []sqlbuilder.JoinClause{}
	if len(ir.Cubes) > 1 {
		// 第二个 cube 的 SQL 作为 subquery
		other := ir.Cubes[1]
		otherFromSQL := renderSecurity(other.SQL, sec)
		otherFromIdent := renderIdent(dialect, other.Name)
		// 找到 join 配置
		primaryJoin := findJoin(ir.Cube, other.Name)
		if primaryJoin == nil {
			return nil, fmt.Errorf("compiler: no join from %s to %s", ir.Cube.Name, other.Name)
		}
		// parseJoinSQL 把 {CUBE} 替换成 cube name,返回 sqlbuilder.Expr
		joinOn := parseJoinSQL(primaryJoin.SQL, ir.Cube.Name, other.Name)
		joins = append(joins, sqlbuilder.JoinClause{
			Type:  "LEFT", // 阉割版默认 LEFT
			Table: &sqlbuilder.TableRef{
				Name:   other.Name,
				Source: other.Name,
			},
			Alias: other.Name,
			On:    joinOn,
		})
		_ = otherFromSQL
		_ = otherFromIdent
	}
	stmt.Joins = joins

	// 7. 渲染
	r := sqlbuilder.NewRenderer(dialect)
	sqlStr, err := r.RenderSelect(stmt)
	if err != nil {
		return nil, err
	}

	// 8. 把 cube.SQL 作为 subquery 嵌入
	fromRe := regexp.QuoteMeta(renderIdent(dialect, cube.Name))
	pattern := "(?i)FROM " + fromRe
	sqlStr = reFirstMatch(sqlStr, pattern, "FROM ("+fromSQL+") AS "+renderIdent(dialect, cube.Name))

	// 9. W5: 把第二个 cube 也嵌入为 subquery
	if len(ir.Cubes) > 1 {
		other := ir.Cubes[1]
		otherFromSQL := renderSecurity(other.SQL, sec)
		otherIdent := renderIdent(dialect, other.Name)
		// 替换 LEFT JOIN "other_name" → LEFT JOIN (other_sql) AS "other_name"
		otherPattern := "(?i)LEFT JOIN " + regexp.QuoteMeta(otherIdent)
		otherRepl := "LEFT JOIN (" + otherFromSQL + ") AS " + otherIdent
		sqlStr = reFirstMatch(sqlStr, otherPattern, otherRepl)
	}

	// 8. 收集参数:filter 里的 Values + dateRange
	args := []any{}
	for _, f := range ir.Filters {
		args = append(args, f.Values...)
	}
	for _, td := range ir.TimeDimensions {
		for _, dr := range td.DateRange {
			args = append(args, dr)
		}
	}

	return &Pass3Result{SQL: sqlStr, Args: args}, nil
}

func renderIdent(d sqlbuilder.Dialect, name string) string {
	open, close := `"`, `"`
	switch d {
	case sqlbuilder.DialectMySQL:
		open, close = "`", "`"
	case sqlbuilder.DialectMSSQL:
		open, close = "[", "]"
	}
	return open + name + close
}

// renderSecurity 把 ${SECURITY.x} 替换成转义后的值
//
//   cube.SQL 里写: "... WHERE tenant_id = '${SECURITY.tenant_id}'"
//   sec.Get("tenant_id") = "acme"
//   替换结果:        "... WHERE tenant_id = 'acme'"
//
//   特殊字符处理: 值内的单引号要转义为 ''(SQL 标准)
//   如果 sec 没拿到 key,替换为 '0' 兜底(阉割版)
func renderSecurity(sql string, sec *security.Context) string {
	re := regexp.MustCompile(`\$\{SECURITY\.([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	return re.ReplaceAllStringFunc(sql, func(m string) string {
		key := m[len("${SECURITY.") : len(m)-1]
		if sec == nil {
			return "'0'"
		}
		if v, ok := sec.Get(key); ok {
			// 转义单引号: O'Brien → O''Brien
			return strings.ReplaceAll(v, "'", "''")
		}
		return "'0'"
	})
}

// parseSegment 解析 segment SQL(把 {CUBE} 替换成 cube 实际名)
func parseSegment(sql string, cubeName string) sqlbuilder.Expr {
	// 简化:把 {CUBE} 替换成 "cube_name"
	s := strings.ReplaceAll(sql, "{CUBE}", cubeName)
	return sqlbuilder.Raw(s)
}

// parseJoinSQL 解析 join SQL(把 {CUBE} 替换成主 cube, {otherCube} 替换成关联 cube)
//   cube.js 写法: "{CUBE}.user_id = {users}.id"
//   实际: cube_name → "orders", otherName → "users"
func parseJoinSQL(sql string, cubeName, otherCubeName string) sqlbuilder.Expr {
	s := strings.ReplaceAll(sql, "{CUBE}", cubeName)
	s = strings.ReplaceAll(s, "{"+otherCubeName+"}", otherCubeName)
	// 简化:直接作为 Raw 注入(用户 trust)
	return sqlbuilder.Raw(s)
}

// reFirstMatch 用正则替换第一次出现
func reFirstMatch(s, pattern, repl string) string {
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllString(s, repl)
}

// rewriteTimeExprForDialect 把标准 DATE_TRUNC(expr, granularity) 改写成方言特定写法
//   PG/CH:  DATE_TRUNC('day', col)
//   MySQL:  DATE_FORMAT(col, '%Y-%m-%d')
//   SQLite: strftime('%Y-%m-%d', col)
//   MSSQL:  DATEADD(dd, DATEDIFF(dd, 0, col), 0)   2008 R2 兼容
//
// 阉割版:只对有 granularity 的 td 处理;没 granularity 直接返回原 expr(原样列)
func rewriteTimeExprForDialect(expr sqlbuilder.Expr, dialect sqlbuilder.Dialect, granularity string) sqlbuilder.Expr {
	if granularity == "" {
		return expr
	}
	// 我们知道 expr 是 DateTrunc(granularity, col) 形式
	// 通过类型断言拿 granularity 参数和列
	dt, ok := expr.(*sqlbuilder.Func)
	if !ok || dt.Name != "DATE_TRUNC" || len(dt.Args) != 2 {
		return expr
	}
	colArg := dt.Args[1]
	switch dialect {
	case sqlbuilder.DialectPostgres, sqlbuilder.DialectCH:
		// PG/CH 原生支持 DATE_TRUNC
		return expr
	case sqlbuilder.DialectMySQL:
		// DATE_FORMAT(col, fmt)
		fmtStr := granularityToMySQLFormat(granularity)
		return &sqlbuilder.Func{Name: "DATE_FORMAT", Args: []sqlbuilder.Expr{colArg, sqlbuilder.Lit(fmtStr)}}
	case sqlbuilder.DialectSQLite:
		fmtStr := granularityToSQLiteFormat(granularity)
		return &sqlbuilder.Func{Name: "strftime", Args: []sqlbuilder.Expr{sqlbuilder.Lit(fmtStr), colArg}}
	case sqlbuilder.DialectMSSQL:
		// DATEADD(dd, DATEDIFF(dd, 0, col), 0) 形式
		// 注意:DATEADD 第一参数是 datepart 标识符(如 dd/mm/yy),不是字符串
		// SQL Server 2008 R2 不接受 DATEADD('dd', ...)
		dp := sqlbuilder.DatePart_(granularityToMSSQLUnit(granularity))
		return &sqlbuilder.Func{Name: "DATEADD", Args: []sqlbuilder.Expr{
			dp,
			&sqlbuilder.Func{Name: "DATEDIFF", Args: []sqlbuilder.Expr{
				dp,
				sqlbuilder.Lit(0),
				colArg,
			}},
			sqlbuilder.Lit(0),
		}}
	}
	return expr
}

func granularityToMySQLFormat(g string) string {
	switch g {
	case "hour":
		return "%Y-%m-%d %H:00:00"
	case "day":
		return "%Y-%m-%d"
	case "week":
		return "%x-W%v"
	case "month":
		return "%Y-%m"
	case "quarter":
		return "%Y-Q%q" // MySQL 5.7+ 支持
	case "year":
		return "%Y"
	}
	return "%Y-%m-%d"
}

func granularityToSQLiteFormat(g string) string {
	switch g {
	case "hour":
		return "%Y-%m-%d %H:00:00"
	case "day":
		return "%Y-%m-%d"
	case "week":
		return "%Y-%W"
	case "month":
		return "%Y-%m"
	case "quarter":
		// SQLite 没有原生 quarter,用 year + month/3
		// 简化: 返回 YYYY-Q
		// 实际需要 strftime + 表达式拼装,W2 阶段先返回年初
		return "%Y"
	case "year":
		return "%Y"
	}
	return "%Y-%m-%d"
}

func granularityToMSSQLUnit(g string) string {
	switch g {
	case "hour":
		return "hh"
	case "day":
		return "dd"
	case "week":
		return "wk" // 2008 R2 支持
	case "month":
		return "mm"
	case "quarter":
		return "qq"
	case "year":
		return "yy"
	}
	return "dd"
}

// rewriteOrderByForDialect 把 ORDER BY 中 time dim 的 DateTrunc 表达式改成方言特定写法
//   MSSQL: DATE_TRUNC('day', col) → DATEADD(dd, DATEDIFF(dd, 0, col), 0)
//   跟 SELECT/GROUP BY 保持一致(否则 MSSQL 报 'DATE_TRUNC' 不是可以识别的内置函数名称)
func rewriteOrderByForDialect(in []sqlbuilder.OrderBy, tds []ResolvedTimeDimension, dialect sqlbuilder.Dialect) []sqlbuilder.OrderBy {
	if len(tds) == 0 {
		return in
	}
	out := make([]sqlbuilder.OrderBy, 0, len(in))
	for _, ob := range in {
		rewritten := ob
		// 尝试找到匹配的 time dimension
		for _, td := range tds {
			if exprsEqual(ob.Expr, td.Expr) {
				rewritten.Expr = rewriteTimeExprForDialect(td.Expr, dialect, td.Granularity)
				break
			}
		}
		out = append(out, rewritten)
	}
	return out
}

// exprsEqual 简单比较两个 expr 是不是同一个(按 fmt 字符串比)
//   够用,因为 ORDER BY 的 expr 跟 time dim expr 来自同一个 resolveOrderField
func exprsEqual(a, b sqlbuilder.Expr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
