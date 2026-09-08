package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/tinkler/cube-agent-server/internal/api/middleware"
	"github.com/tinkler/cube-agent-server/internal/engine/source"
)

// SourceDirectDeps /admin/source-direct 依赖
//   实际是 *engine.Executor (有 DataSourceConfigs + 通过 source 连接)
type SourceDirectDeps struct {
	// 拿数据源的接口 (返回所有 datasource 配置, 找名字匹配的)
	DataSources func() []*source.DataSourceConfig
	// 拿具体数据源 (在 source.Registry 里有; 这里用 Executor 的查询能力)
	// ⚠️ 当前 Executor 抽象没有暴露 "按名字拿 DataSource" 的方法
	//   我们用 DSN 直接构造查询 (复用 source.NewRegistry + mssql driver)
	SourceRegistry func() *source.Registry
	Logger         *zap.Logger
}

// SourceDirectRequest 接受参数
//   约束: 只允许白名单 (table / column / op) 防 SQL 注入
//   只支持 aggregate: sum/avg/count/max/min
//   时间过滤: since/until 用 mssql datetime 格式 (2006-01-02 15:04:05)
type SourceDirectRequest struct {
	Datasource string `json:"datasource"`            // 默认 "hbpos"
	Table      string `json:"table"`                 // 白名单: t_rm_saleflow / t_im_flow / t_im_branch_stock
	Op         string `json:"op"`                    // 白名单: sum / avg / count / max / min
	Column     string `json:"column"`                // 白名单: 思迅物理列名 (按 table 限定)
	Since      string `json:"since"`                 // 起始时间 mssql datetime
	Until      string `json:"until"`                 // 结束时间 mssql datetime
	Where      string `json:"where,omitempty"`       // 额外 WHERE (走白名单, 暂不开放)
	GroupBy    string `json:"group_by,omitempty"`    // 暂不实现
}

// 白名单 (防 SQL 注入 + 限制攻击面)
var (
	allowedTables = map[string]bool{
		"t_rm_saleflow":       true, // 销售流水 (含退货, 跟 sales_with_refund 对账)
		"t_im_flow":           true, // 采购/出入库流水 (跟 purchases 对账)
		"t_im_branch_stock":   true, // 库存快照 (跟 inventory_current 对账)
	}
	allowedOps = map[string]bool{
		"sum": true, "avg": true, "count": true, "max": true, "min": true,
	}
	// table → 允许聚合的列白名单 (防 SELECT * 和注入)
	allowedColumnsByTable = map[string]map[string]bool{
		"t_rm_saleflow": {
			"sale_money": true, "sale_qnty": true, "in_price": true,
			"flow_id": true, "oper_date": true,
		},
		"t_im_flow": {
			"real_qty": true, "cost_price": true, "sheet_amt": true,
			"purchase_tax": true, "sale_tax": true,
		},
		"t_im_branch_stock": {
			"stock_qty": true, "route_qty": true, "avg_cost": true,
		},
	}
)

// SourceDirectResponse 返回
type SourceDirectResponse struct {
	Datasource  string  `json:"datasource"`
	Table       string  `json:"table"`
	Op          string  `json:"op"`
	Column      string  `json:"column"`
	Value       float64 `json:"value"`
	RowsCounted int     `json:"rows_counted"`
	DurationMs  int64   `json:"duration_ms"`
	QuerySQL    string  `json:"query_sql"` // 调试用, 暴露实际 SQL (用户/agent review 友好)
}

// SourceDirect POST /admin/source-direct
//   2026-09-09 W1.6: 给 collect-ai freshcheck C7 同步校验用
//   强制白名单 (table/column/op), 防 SQL 注入
//   走真实 SQL Server aggregate, 不走 cube 抽象层
//
// 用例: collect-ai 端调
//   POST /admin/source-direct
//   {"datasource":"hbpos","table":"t_rm_saleflow","op":"sum","column":"sale_money","since":"2026-09-08 00:00:00","until":"2026-09-08 23:59:59"}
func SourceDirect(deps SourceDirectDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req SourceDirectRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "bad json: " + err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}

		// 1. 默认 datasource = hbpos
		if req.Datasource == "" {
			req.Datasource = "hbpos"
		}

		// 2. 白名单校验
		if !allowedTables[req.Table] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "table 不在白名单 (允许: t_rm_saleflow / t_im_flow / t_im_branch_stock)",
				"got":        req.Table,
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		if !allowedOps[req.Op] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "op 不在白名单 (允许: sum/avg/count/max/min)",
				"got":        req.Op,
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		cols, ok := allowedColumnsByTable[req.Table]
		if !ok || !cols[req.Column] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "column 不在该 table 白名单",
				"table":      req.Table,
				"column":     req.Column,
				"allowed":    columnList(cols),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}

		// 3. 找数据源
		var matchedCfg *source.DataSourceConfig
		for _, ds := range deps.DataSources() {
			if ds.Name == req.Datasource {
				matchedCfg = ds
				break
			}
		}
		if matchedCfg == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error":      "datasource 不存在",
				"datasource": req.Datasource,
				"request_id": middleware.GetRequestID(c),
			})
			return
		}

		// 4. 构造数据源连接 (复用 source.Registry)
		registry := deps.SourceRegistry()
		if registry == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "source registry not configured",
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		ds, err := registry.Build(matchedCfg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "build datasource: " + err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		defer ds.Close()

		// 5. 构造 SQL
		// ⚠️ 安全: column/table/op 都是白名单, 不可注入
		//   since/until 是 mssql datetime 字符串, 也做基本格式校验
		sql, args := buildAggregateSQL(req)
		if sql == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "时间范围缺失或格式错 (since/until 必填 mssql datetime)",
				"request_id": middleware.GetRequestID(c),
			})
			return
		}

		// 6. 执行
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		start := time.Now()
		result, err := ds.Query(ctx, sql, args...)
		durationMs := time.Since(start).Milliseconds()
		if err != nil {
			deps.Logger.Error("source-direct query failed",
				zap.String("datasource", req.Datasource),
				zap.String("sql", sql),
				zap.Error(err),
			)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "query failed: " + err.Error(),
				"query_sql":  sql,
				"request_id": middleware.GetRequestID(c),
			})
			return
		}

		// 7. 解析结果
		value, rowsCounted := extractValue(result)
		deps.Logger.Info("source-direct ok",
			zap.String("datasource", req.Datasource),
			zap.String("table", req.Table),
			zap.String("op", req.Op),
			zap.String("column", req.Column),
			zap.Float64("value", value),
			zap.Int("rows", rowsCounted),
			zap.Int64("duration_ms", durationMs),
		)
		c.JSON(http.StatusOK, SourceDirectResponse{
			Datasource:  req.Datasource,
			Table:       req.Table,
			Op:          req.Op,
			Column:      req.Column,
			Value:       value,
			RowsCounted: rowsCounted,
			DurationMs:  durationMs,
			QuerySQL:    sql,
		})
	}
}

// buildAggregateSQL 构造 aggregate SQL
//   ⚠️ 安全: table/column/op 已白名单, since/until 用占位符 (不拼接字符串)
func buildAggregateSQL(req SourceDirectRequest) (string, []any) {
	if req.Since == "" || req.Until == "" {
		return "", nil
	}
	// 验证时间格式 (防止注入)
	// 接受: "2006-01-02 15:04:05" 或 "2006-01-02"
	if !isValidDateTime(req.Since) || !isValidDateTime(req.Until) {
		return "", nil
	}
	expr := fmt.Sprintf("%s(%s)", strings.ToUpper(req.Op), req.Column)
	// 2008 R2: 显式用 schema dbo
	sql := fmt.Sprintf("SELECT %s AS value, COUNT(*) AS row_count FROM dbo.%s WHERE oper_date >= ? AND oper_date <= ?",
		expr, req.Table)
	args := []any{req.Since, req.Until}
	// 可选额外 WHERE (留口子, 当前白名单为空)
	if req.Where != "" {
		// TODO 未来: 解析 + 白名单匹配
		_ = req.Where
	}
	return sql, args
}

// isValidDateTime 校验 mssql datetime 格式
//   接受: "2006-01-02 15:04:05" / "2006-01-02"
func isValidDateTime(s string) bool {
	if len(s) < 10 {
		return false
	}
	// 简单校验: 前 10 字符是 "YYYY-MM-DD"
	if s[4] != '-' || s[7] != '-' {
		return false
	}
	// 后面可选 ' HH:MM:SS'
	if len(s) > 10 {
		if s[10] != ' ' {
			return false
		}
		if len(s) != 19 {
			return false
		}
	}
	return true
}

// extractValue 从 Result 提取 sum/avg/count/max/min 数值
func extractValue(result *source.Result) (float64, int) {
	if result == nil || len(result.Rows) == 0 {
		return 0, 0
	}
	row := result.Rows[0]
	v, _ := row["value"].(float64)
	c, _ := row["row_count"].(float64)
	return v, int(c)
}

// columnList 把白名单 map 转 []string (用于错误信息)
func columnList(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
