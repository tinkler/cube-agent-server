package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tinkler/cube-agent-server/internal/api/middleware"
	"github.com/tinkler/cube-agent-server/internal/compiler"
	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	rolluppkg "github.com/tinkler/cube-agent-server/internal/rollup"
	"github.com/tinkler/cube-agent-server/internal/schema"
	"github.com/tinkler/cube-agent-server/internal/security"
	"github.com/tinkler/cube-agent-server/internal/engine/source"
)

// QueryExecutor /v1/load 的执行器
// W3 阶段由 engine.Executor 实现
// W2 阶段 stub:nil(返回 501)
type QueryExecutor interface {
	Execute(c *gin.Context, sql string, args []any) (any, error)
}

// QueryDeps /v1/load /v1/sql /v1/dry-run 共享依赖
// W3 起:Dialect 改为 LookupDialect 函数(per-cube dialect,从 datasource 读)
//        旧 Dialect 字段保留作为全局兜底
type QueryDeps struct {
	Registry      *schema.Registry
	Dialect       sqlbuilder.Dialect
	LookupDialect func(cubeName string) sqlbuilder.Dialect
	Executor      QueryExecutor // W3 接入,W2 阶段可为 nil
}

// resolveDialect 拿 cube 实际 dialect
func (d QueryDeps) resolveDialect(cubeName string) sqlbuilder.Dialect {
	if d.LookupDialect != nil {
		if dl := d.LookupDialect(cubeName); dl != "" {
			return dl
		}
	}
	return d.Dialect
}

// Sql POST /v1/sql
// 编译 query → 返回 SQL 不执行
func Sql(deps QueryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, status, body := compileQuery(c, deps)
		if result == nil {
			c.JSON(status, body)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"sql":        result.SQL,
			"args":       result.Args,
			"request_id": middleware.GetRequestID(c),
		})
	}
}

// DryRun POST /v1/dry-run
// 编译 query → 校验 → 不执行不返 SQL
func DryRun(deps QueryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "read body",
				"detail":     err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		ir, err := compiler.Pass1(body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "query parse failed",
				"detail":     err.Error(),
				"valid":      false,
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		sec := security.FromRequest(headerMap(c))
		resolved, err := compiler.Pass2(ir, deps.Registry)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"valid":      false,
				"error":      err.Error(),
				"stage":      "pass2",
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		dialect := deps.resolveDialect(resolved.PrimaryCube)
		_, err = compiler.Pass3(resolved, dialect, sec)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"valid":      false,
				"error":      err.Error(),
				"stage":      "pass3",
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"valid":      true,
			"request_id": middleware.GetRequestID(c),
		})
	}
}

// Load POST /v1/load
// 编译 query → 执行 → 返回结果
// W2 阶段:Executor nil 时返回 501(W3 接入)
func Load(deps QueryDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 复用 compileQuery 走 Pass1/Pass2/Pass3
		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "read body",
				"detail":     err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		ir, err := compiler.Pass1(body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "query parse failed",
				"detail":     err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		sec := security.FromRequest(headerMap(c))
		resolved, err := compiler.Pass2(ir, deps.Registry)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "query resolve failed",
				"detail":     err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		// 把 cube name 塞到 context,给 engine.Executor 用
		if resolved.PrimaryCube != "" {
			c.Set("primary_cube", resolved.PrimaryCube)
		}
		// W3: per-cube dialect(从 datasource 查)
		dialect := deps.resolveDialect(resolved.PrimaryCube)
		result, err := compiler.Pass3(resolved, dialect, sec)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "sql generate failed",
				"detail":     err.Error(),
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		if deps.Executor == nil {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error":      "query executor not configured (W3 接入)",
				"sql":        result.SQL,
				"args":       result.Args,
				"request_id": middleware.GetRequestID(c),
			})
			return
		}
		rawData, err := deps.Executor.Execute(c, result.SQL, result.Args)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":      "query execute failed",
				"detail":     err.Error(),
				"sql":        result.SQL,
				"request_id": middleware.GetRequestID(c),
			})
			return
		}

		// 阉割版扩展:timeRollup — Go 端对 time dim 额外做多粒度 rollup
		// 避免 3 次 DB round-trip(1 次 DB 拿 day 数据,Go 内存 rollup 到 week/month/year)
		// 减轻数据源 DB 运算压力(尤其老 SQL Server 2008 R2)
		resp := gin.H{
			"data":       rawData,
			"sql":        result.SQL,
			"request_id": middleware.GetRequestID(c),
		}
		if len(ir.Query.TimeRollup) > 0 {
			rollups, rerr := applyTimeRollup(ir, resolved, rawData)
			if rerr != nil {
				// rollup 失败不影响主结果,只警告
				resp["rollup_warning"] = rerr.Error()
			} else if rollups != nil {
				resp["rollups"] = rollups
			}
		}
		c.JSON(http.StatusOK, resp)
	}
}

// applyTimeRollup 对 query 的 day 数据做内存 rollup
//   当前只支持单 time dim;如果 query 没有 time dim 或多于 1 个,返回 nil
//   measure 列:从 ResolvedIR.Measures 拿
//   time dim 列:从 ResolvedIR.TimeDimensions[0] 拿
func applyTimeRollup(ir *compiler.IR, resolved *compiler.ResolvedIR, rawData any) (map[string][]map[string]any, error) {
	if ir == nil || ir.Query == nil {
		return nil, nil
	}
	if resolved == nil {
		return nil, nil
	}
	if len(resolved.TimeDimensions) == 0 {
		return nil, nil
	}
	if len(resolved.TimeDimensions) > 1 {
		return nil, fmt.Errorf("timeRollup only supports single time dim, got %d", len(resolved.TimeDimensions))
	}
	td := resolved.TimeDimensions[0]
	if td.Granularity == "" {
		return nil, fmt.Errorf("timeRollup requires time dim with granularity, got empty")
	}
	// 拿 rows
	res, ok := rawData.(*source.Result)
	if !ok || res == nil || len(res.Rows) == 0 {
		return nil, nil
	}
	// 拿 measure 列名
	measureCols := make([]string, 0, len(resolved.Measures))
	for _, m := range resolved.Measures {
		measureCols = append(measureCols, m.Name)
	}
	if len(measureCols) == 0 {
		return nil, nil
	}
	return rolluppkg.Rollup(res.Rows, rolluppkg.Options{
		TimeDimCol:         td.Name,
		MeasureCols:        measureCols,
		SourceGranularity:  td.Granularity,
		TargetGranularities: ir.Query.TimeRollup,
	})
}

// compileQuery Pass1+Pass2+Pass3 流程
// 返回 (result, status, body) — result 为 nil 表示有错,body 是响应
func compileQuery(c *gin.Context, deps QueryDeps) (*compiler.Pass3Result, int, gin.H) {
	body, err := c.GetRawData()
	if err != nil {
		return nil, http.StatusBadRequest, gin.H{
			"error":      "read body",
			"detail":     err.Error(),
			"request_id": middleware.GetRequestID(c),
		}
	}
	ir, err := compiler.Pass1(body)
	if err != nil {
		return nil, http.StatusBadRequest, gin.H{
			"error":      "query parse failed",
			"detail":     err.Error(),
			"request_id": middleware.GetRequestID(c),
		}
	}
	sec := security.FromRequest(headerMap(c))
	resolved, err := compiler.Pass2(ir, deps.Registry)
	if err != nil {
		return nil, http.StatusBadRequest, gin.H{
			"error":      "query resolve failed",
			"detail":     err.Error(),
			"request_id": middleware.GetRequestID(c),
		}
	}
	dialect := deps.resolveDialect(resolved.PrimaryCube)
	res, err := compiler.Pass3(resolved, dialect, sec)
	if err != nil {
		return nil, http.StatusInternalServerError, gin.H{
			"error":      "sql generate failed",
			"detail":     err.Error(),
			"request_id": middleware.GetRequestID(c),
		}
	}
	return res, http.StatusOK, nil
}

func headerMap(c *gin.Context) map[string]string {
	out := map[string]string{}
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}
