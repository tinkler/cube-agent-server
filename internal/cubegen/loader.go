// Package cubegen - yaegi 加载器
//
// 用 yaegi 解释执行 .go 插件,提取 BuildSQL 调用
// 不用 Go plugin 包(plugin 加载/版本耦合限制)
package cubegen

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"

	"github.com/tinkler/cube-agent-server/internal/compiler/sqlbuilder"
	"github.com/tinkler/cube-agent-server/internal/cubegenapi"
	"github.com/tinkler/cube-agent-server/internal/yaegisym"
)

// YaegiLoader 加载器
//   ⚠️ 2026-08-31 fix: 每个 plugin 文件独立 interp,避免 stdlib sym 互相污染
//     原设计:全局共享 l.interp,先加载 supplier_sales → 注入 encoding/json sym
//            后加载 display_restock_window → 报 "json_.go redeclared"
//     修法:interps map[path]*Interpreter,每个文件 fresh interp
type YaegiLoader struct {
	mu         sync.Mutex
	interps    map[string]*interp.Interpreter
	loaded     map[string]string
	fileMTimes map[string]time.Time
}

func NewYaegiLoader() (*YaegiLoader, error) {
	return &YaegiLoader{
		interps:    map[string]*interp.Interpreter{},
		loaded:     map[string]string{},
		fileMTimes: map[string]time.Time{},
	}, nil
}

func newInterp() (*interp.Interpreter, error) {
	i := interp.New(interp.Options{GoPath: detectGoPath()})
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, fmt.Errorf("yaegi: use stdlib: %w", err)
	}
	if err := i.Use(yaegisym.Symbols); err != nil {
		return nil, fmt.Errorf("yaegi: use cubegenapi+sqlbuilder: %w", err)
	}
	return i, nil
}

// LoadFile 加载 .go 源文件(只验证语法,真正调用走 CallBuildSQL)
//   每个文件独立 interp,避免 cross-plugin 污染
func (l *YaegiLoader) LoadFile(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("yaegi: stat %s: %w", path, err)
	}
	mtime := info.ModTime()

	if status, ok := l.loaded[path]; ok && status == "ok" {
		if l.fileMTimes[path].Equal(mtime) {
			return nil
		}
	}

	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("yaegi: read %s: %w", path, err)
	}

	// 每文件 fresh interp(不再跨文件共享)
	interp, err := newInterp()
	if err != nil {
		return err
	}
	if _, err := interp.Eval(string(src)); err != nil {
		l.loaded[path] = fmt.Sprintf("eval err: %v", err)
		return fmt.Errorf("yaegi: eval %s: %w", path, err)
	}
	// 验证 Build 函数存在
	if _, err := interp.Eval("Build"); err != nil {
		l.loaded[path] = fmt.Sprintf("Build missing: %v", err)
		return fmt.Errorf("yaegi: Build not found in %s: %w", path, err)
	}

	l.interps[path] = interp
	l.loaded[path] = "ok"
	l.fileMTimes[path] = mtime
	return nil
}

// jsonPlan 跟插件的 JSONPlan 对齐(内部传输协议)
type jsonPlan struct {
	From  *struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"from"`
	Joins []struct {
		Type  string `json:"type"`
		Table struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"table"`
		On string `json:"on"`
	} `json:"joins"`
	Where string `json:"where"`
	Cols  []string `json:"cols"` // 显式 SELECT 列(Raw SQL 字符串)
}

// CallBuildSQL 通过 JSON 序列化跨 yaegi interpreter 边界传 ctx 和 plan
// 规避 Go interface 跨 interpreter 问题(yaegi 内部用 valueInterface 包装)
func (l *YaegiLoader) CallBuildSQL(path string, ctx *cubegenapi.BuildContext) (*cubegenapi.SQLPlan, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	interp, ok := l.interps[path]
	if !ok {
		return nil, fmt.Errorf("yaegi: plugin %s not loaded", path)
	}

	ctxJSON, err := json.Marshal(ctx)
	if err != nil {
		return nil, fmt.Errorf("ctx json: %w", err)
	}

	// 调 plugin:Build(ctxJSON)
	src, err := interp.Eval(fmt.Sprintf(`Build(%q)`, string(ctxJSON)))
	if err != nil {
		return nil, fmt.Errorf("yaegi: call Build: %w", err)
	}
	if !src.IsValid() {
		return nil, fmt.Errorf("yaegi: Build returned invalid value")
	}

	// yaegi 返回 string,直接拿
	planJSON, ok := src.Interface().(string)
	if !ok {
		return nil, fmt.Errorf("yaegi: Build returned %T, want string", src.Interface())
	}

	// 反序列化
	var jp jsonPlan
	if err := json.Unmarshal([]byte(planJSON), &jp); err != nil {
		return nil, fmt.Errorf("plan json unmarshal: %w", err)
	}

	// 构造 *cubegenapi.SQLPlan
	plan := &cubegenapi.SQLPlan{}
	if jp.From != nil {
		plan.From = &sqlbuilder.TableRef{Name: jp.From.Name, Source: jp.From.Source}
	}
	for _, j := range jp.Joins {
		plan.Joins = append(plan.Joins, sqlbuilder.JoinClause{
			Type:  j.Type,
			Table: &sqlbuilder.TableRef{Name: j.Table.Name, Source: j.Table.Source},
			On:    sqlbuilder.Raw(j.On),
		})
	}
	if jp.Where != "" {
		plan.Where = sqlbuilder.Raw(jp.Where)
	}
	// 显式列
	for _, c := range jp.Cols {
		plan.Cols = append(plan.Cols, sqlbuilder.SelectColumn{Expr: sqlbuilder.Raw(c)})
	}
	return plan, nil
}

// detectGoPath 给 yaegi 用的 GOPATH
func detectGoPath() string {
	return `F:\go`
}
