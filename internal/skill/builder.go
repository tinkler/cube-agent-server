// Package skill AI Plugin Builder 入口
// 7 步引导流程,见 docs/AI_SKILL.md
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/tinkler/cube-agent-server/internal/engine/source"
	"github.com/tinkler/cube-agent-server/internal/skill/analyzer"
	"github.com/tinkler/cube-agent-server/internal/skill/datasource"
	"github.com/tinkler/cube-agent-server/internal/skill/llm"
	"github.com/tinkler/cube-agent-server/internal/schema"
)

// Automation 自动化档位
type Automation string

const (
	Auto     Automation = "auto"      // 全自动
	SemiAuto Automation = "semi-auto" // 半自动(默认)
	Strict   Automation = "strict"    // 严格
)

// Session 一次引导会话状态
type Session struct {
	ID         string    `json:"id"`
	Step       int       `json:"step"`
	Automation Automation `json:"automation"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// Step 1
	Intent string `json:"intent,omitempty"`

	// Step 2
	Datasource string `json:"datasource,omitempty"`

	// Step 3
	Meta *datasource.Meta `json:"meta,omitempty"`

	// Step 4
	Design *Design `json:"design,omitempty"`

	// Step 5
	Draft *Draft `json:"draft,omitempty"`

	// Step 6
	Validation *ValidationResult `json:"validation,omitempty"`

	// Step 7
	PublishedPath string `json:"published_path,omitempty"`

	Done   bool   `json:"done"`
	Error  string `json:"error,omitempty"`
}

// Design Step 4 输出
type Design struct {
	CubeName        string   `json:"cube_name"`
	CubeDescription string   `json:"cube_description"`
	SQLTemplate     string   `json:"sql_template"`
	PrimaryKey      string   `json:"primary_key"`
	Measures        []string `json:"measures"`      // measure 名字列表
	Dimensions      []string `json:"dimensions"`    // dimension 名字列表
	Segments        []string `json:"segments"`      // segment 名字列表
	FilterHints     []string `json:"filter_hints"`  // 用户加的 filter 提示
}

// Draft Step 5 输出:生成的 plugin.yaml
type Draft struct {
	YAML string `json:"yaml"`
	Path string `json:"path"` // 暂存路径
}

// ValidationResult Step 6 输出
type ValidationResult struct {
	OK         bool     `json:"ok"`
	SampleSQL  string   `json:"sample_sql"`
	SampleRows int      `json:"sample_rows"`
	Warnings   []string `json:"warnings"`
	Error      string   `json:"error,omitempty"`
}

// Builder AI skill 引导器
type Builder struct {
	llm        llm.Client
	analyzer   *analyzer.Analyzer
	introspect *datasource.Introspector
	srcReg     *source.Registry
	pluginMgr  pluginReloader
	pluginsDir string
	cacheDir   string
	logger     *zap.Logger

	mu       sync.Mutex
	sessions map[string]*Session
}

// pluginReloader 触发 plugin 重新加载(从 plugin.Manager 抽出)
type pluginReloader interface {
	Reload() error
}

// NewBuilder 构造
func NewBuilder(
	c llm.Client,
	introspect *datasource.Introspector,
	srcReg *source.Registry,
	pluginMgr pluginReloader,
	pluginsDir string,
	cacheDir string,
	logger *zap.Logger,
) *Builder {
	return &Builder{
		llm:        c,
		analyzer:   analyzer.New(c),
		introspect: introspect,
		srcReg:     srcReg,
		pluginMgr:  pluginMgr,
		pluginsDir: pluginsDir,
		cacheDir:   cacheDir,
		logger:     logger,
		sessions:   map[string]*Session{},
	}
}

// ============================================================
// Session 生命周期
// ============================================================

// Start 启动新会话
func (b *Builder) Start(intent string, auto Automation) (*Session, error) {
	s := &Session{
		ID:         uuid.NewString(),
		Step:       1,
		Automation: auto,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Intent:     intent,
	}
	// Step 1: intent 理解(W4 简化:直接收 intent)
	s.Step = 2
	b.mu.Lock()
	b.sessions[s.ID] = s
	b.mu.Unlock()
	b.persist(s)
	return s, nil
}

// Get 取会话
func (b *Builder) Get(id string) (*Session, error) {
	b.mu.Lock()
	s, ok := b.sessions[id]
	b.mu.Unlock()
	if !ok {
		// 尝试从磁盘恢复
		loaded, err := b.loadFromDisk(id)
		if err != nil {
			return nil, fmt.Errorf("session %q not found", id)
		}
		b.mu.Lock()
		b.sessions[id] = loaded
		b.mu.Unlock()
		return loaded, nil
	}
	return s, nil
}

// List 列出所有会话
func (b *Builder) List() []*Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*Session, 0, len(b.sessions))
	for _, s := range b.sessions {
		out = append(out, s)
	}
	return out
}

// ============================================================
// 7 步流程
// ============================================================

// Step2Datasource 选择数据源
func (b *Builder) Step2Datasource(sessionID, dsName string) (*Session, error) {
	s, err := b.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if s.Step < 2 {
		return nil, fmt.Errorf("session not at step 2")
	}
	s.Datasource = dsName
	s.UpdatedAt = time.Now()
	b.persist(s)
	return s, nil
}

// Step3Analyze 触发 introspect + LLM 分析
func (b *Builder) Step3Analyze(sessionID string) (*Session, error) {
	s, err := b.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if s.Datasource == "" {
		return nil, fmt.Errorf("session: datasource not set (step 2 required)")
	}
	if b.introspect == nil {
		return nil, fmt.Errorf("introspect: builder not configured with introspector")
	}

	// 1. 优先读缓存
	cachePath := b.metaCachePath(s.Datasource)
	if data, err := os.ReadFile(cachePath); err == nil {
		var meta datasource.Meta
		if err := yamlUnmarshal(data, &meta); err == nil {
			s.Meta = &meta
			s.Step = 4
			s.UpdatedAt = time.Now()
			b.persist(s)
			return s, nil
		}
	}

	// 2. introspect(introspect 60s 够)
	introspectCtx, cancel1 := newCtx(60 * time.Second)
	defer cancel1()
	meta, err := b.introspect.Introspect(introspectCtx, s.Datasource)
	if err != nil {
		s.Error = err.Error()
		return s, err
	}

	// 3. LLM 分析(独立 ctx,229 张表 prompt 慢,180s 起步)
	if b.llm != nil {
		llmCtx, cancel2 := newCtx(180 * time.Second)
		defer cancel2()
		if err := b.analyzer.Analyze(llmCtx, meta, s.Intent); err != nil {
			b.logger.Warn("LLM analyze failed, continue with raw meta", zap.Error(err))
		}
	}

	// 4. 写缓存
	s.Meta = meta
	if data, err := yamlMarshal(meta); err == nil {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		_ = os.WriteFile(cachePath, data, 0o644)
	}

	s.Step = 4
	s.UpdatedAt = time.Now()
	b.persist(s)
	return s, nil
}

// Step4Design 设计 cube
// W4 简化版:用户直接给 cube name + SQL + 字段名,LLM 补全 description
func (b *Builder) Step4Design(sessionID string, design *Design) (*Session, error) {
	s, err := b.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if s.Meta == nil {
		return nil, fmt.Errorf("session: meta not set (step 3 required)")
	}
	if design.CubeName == "" {
		return nil, fmt.Errorf("design: cube_name required")
	}
	s.Design = design
	s.Step = 5
	s.UpdatedAt = time.Now()
	b.persist(s)
	return s, nil
}

// Step5Generate 生成 plugin.yaml
func (b *Builder) Step5Generate(sessionID string) (*Session, error) {
	s, err := b.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if s.Design == nil {
		return nil, fmt.Errorf("session: design not set (step 4 required)")
	}
	// 简化:根据 Design 直接生成 YAML(真实实现会用 LLM 辅助)
	yaml := buildPluginYAML(s)
	s.Draft = &Draft{YAML: yaml, Path: filepath.Join(b.pluginsDir, s.Design.CubeName)}
	s.Step = 6
	s.UpdatedAt = time.Now()
	b.persist(s)
	return s, nil
}

// Step6Validate 验证(YAML 解析 + dry-run)
func (b *Builder) Step6Validate(sessionID string) (*Session, error) {
	s, err := b.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if s.Draft == nil {
		return nil, fmt.Errorf("session: draft not set (step 5 required)")
	}
	res := &ValidationResult{OK: true}
	// 1. YAML 解析
	p, err := schema.Load([]byte(s.Draft.YAML))
	if err != nil {
		res.OK = false
		res.Error = "yaml parse: " + err.Error()
		s.Validation = res
		b.persist(s)
		return s, fmt.Errorf("validation: %w", err)
	}
	if err := p.Validate(schema.DefaultValidateOptions()); err != nil {
		res.OK = false
		res.Error = "validate: " + err.Error()
		s.Validation = res
		b.persist(s)
		return s, fmt.Errorf("validation: %w", err)
	}
	res.SampleSQL = p.Spec.Cubes[0].SQL
	s.Validation = res
	s.Step = 7
	s.UpdatedAt = time.Now()
	b.persist(s)
	return s, nil
}

// Step7Publish 写入 plugins 目录 + 触发 reload
func (b *Builder) Step7Publish(sessionID string) (*Session, error) {
	s, err := b.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if s.Draft == nil || s.Validation == nil || !s.Validation.OK {
		return nil, fmt.Errorf("session: draft+valid validation required")
	}
	// 1. 写文件
	pluginDir := filepath.Join(b.pluginsDir, s.Design.CubeName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return s, fmt.Errorf("publish: mkdir: %w", err)
	}
	pluginPath := filepath.Join(pluginDir, "plugin.yaml")
	// 用 UTF-8 写(避免 PowerShell 那种 ANSI 编码)
	if err := writeUTF8(pluginPath, s.Draft.YAML); err != nil {
		return s, fmt.Errorf("publish: write: %w", err)
	}
	s.PublishedPath = pluginPath

	// 2. 触发 reload
	if b.pluginMgr != nil {
		if err := b.pluginMgr.Reload(); err != nil {
			return s, fmt.Errorf("publish: reload: %w", err)
		}
	}

	s.Done = true
	s.UpdatedAt = time.Now()
	b.persist(s)
	b.logger.Info("skill published",
		zap.String("session_id", s.ID),
		zap.String("plugin", s.Design.CubeName),
		zap.String("path", pluginPath),
	)
	return s, nil
}

// Cancel 取消会话
func (b *Builder) Cancel(sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.sessions[sessionID]; !ok {
		return fmt.Errorf("session %q not found", sessionID)
	}
	delete(b.sessions, sessionID)
	return nil
}

// AutoBuild 全自动模式:一气呵成跑完 Step 2-7
// W5 设计:不要求 LLM 也能跑(降级为:Step 4 用 Meta 自动生成 design)
// 有 LLM 时:Step 3 调 LLM 增强,Step 4 用 LLM 推荐 cube 结构
// 无 LLM 时:从 Meta 自动抽取 measure/dimension(简单规则)
func (b *Builder) AutoBuild(intent, dsName string) (*Session, error) {
	// Step 1:start
	s, err := b.Start(intent, Auto)
	if err != nil {
		return nil, err
	}
	// Step 2:datasource
	s, err = b.Step2Datasource(s.ID, dsName)
	if err != nil {
		return s, err
	}
	// Step 3:analyze
	s, err = b.Step3Analyze(s.ID)
	if err != nil {
		return s, err
	}
	// Step 4:design(自动推断)
	design, err := b.inferDesign(s)
	if err != nil {
		return s, fmt.Errorf("auto design: %w", err)
	}
	s, err = b.Step4Design(s.ID, design)
	if err != nil {
		return s, err
	}
	// Step 5:generate
	s, err = b.Step5Generate(s.ID)
	if err != nil {
		return s, err
	}
	// Step 6:validate
	s, err = b.Step6Validate(s.ID)
	if err != nil {
		return s, err
	}
	// Step 7:publish
	s, err = b.Step7Publish(s.ID)
	if err != nil {
		return s, err
	}
	return s, nil
}

// inferDesign 根据 Meta 自动推断 cube 设计
// 简化规则:按 intent 关键词给表打分,选最相关的表;
// 数字列作为 measure,非数字列作为 dimension。
// 这是 W5 的"阉割版自动推断",真实版本会用 LLM 选表。
func (b *Builder) inferDesign(s *Session) (*Design, error) {
	if s.Meta == nil || len(s.Meta.Tables) == 0 {
		return nil, fmt.Errorf("no tables in meta")
	}
	// 按 intent 关键词打分,选最相关的表
	scored := scoreTablesByIntent(s.Meta.Tables, s.Intent)
	t := scored[0].t
	if t.Name == "" {
		return nil, fmt.Errorf("empty table name")
	}
	design := &Design{
		CubeName:        t.Name,
		CubeDescription: t.Description,
		SQLTemplate:     fmt.Sprintf("SELECT * FROM %s", t.Name),
		PrimaryKey:      t.PrimaryKey,
		Measures:        []string{},
		Dimensions:      []string{},
		Segments:        []string{},
	}

	// 列分类
	for _, c := range t.Columns {
		cname := c.Name
		ctype := c.Type
		// 主键作为 dimension
		if cname == t.PrimaryKey {
			design.Dimensions = append(design.Dimensions, cname)
			continue
		}
		// 数字列 → measure
		if isNumericType(ctype) {
			design.Measures = append(design.Measures, cname)
		} else if isTimeType(ctype) {
			// 时间列 → dimension
			design.Dimensions = append(design.Dimensions, cname)
		} else {
			// 其他 → dimension
			design.Dimensions = append(design.Dimensions, cname)
		}
	}

	// 至少 1 个 measure
	if len(design.Measures) == 0 {
		design.Measures = []string{"count"}
	}

	return design, nil
}

// isNumericType 数字类型判断(简化版)
func isNumericType(typ string) bool {
	t := strings.ToLower(typ)
	return strings.Contains(t, "int") || strings.Contains(t, "numeric") ||
		strings.Contains(t, "decimal") || strings.Contains(t, "float") ||
		strings.Contains(t, "double") || strings.Contains(t, "real") ||
		strings.Contains(t, "money") || strings.Contains(t, "smallint") ||
		strings.Contains(t, "bigint") || strings.Contains(t, "tinyint")
}

func isTimeType(typ string) bool {
	t := strings.ToLower(typ)
	return strings.Contains(t, "time") || strings.Contains(t, "date") ||
		strings.Contains(t, "timestamp")
}

// scoredTable 表 + intent 命中分数
type scoredTable struct {
	t datasource.TableMeta
	s int
}

// scoreTablesByIntent 按 intent 关键词打分排序。
// 中文 intent → 提取 2-4 字子串作为关键词;每张表算命中数(表名 10 分/关键词,字段名 2 分/关键词)。
// 空 intent 时,保持原顺序。
func scoreTablesByIntent(tables []datasource.TableMeta, intent string) []scoredTable {
	out := make([]scoredTable, len(tables))
	if intent == "" {
		for i, t := range tables {
			out[i] = scoredTable{t, 0}
		}
		return out
	}
	keywords := extractKeywords(intent)
	for i, t := range tables {
		score := 0
		for _, kw := range keywords {
			if builderContains(t.Name, kw) {
				score += 10
			}
			for _, c := range t.Columns {
				if builderContains(c.Name, kw) {
					score += 2
				}
			}
		}
		out[i] = scoredTable{t, score}
	}
	// 稳定排序:分数高在前(用 sort.SliceStable 保序)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].s > out[j].s
	})
	return out
}

// extractKeywords 中文关键词提取(2-4 字连续子串)
func extractKeywords(intent string) []string {
	runes := []rune(intent)
	seen := map[string]bool{}
	out := []string{}
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

// builderContains unicode 安全的子串包含
func builderContains(s, sub string) bool {
	if sub == "" {
		return true
	}
	sr := []rune(s)
	subr := []rune(sub)
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

// ============================================================
// 持久化
// ============================================================

func (b *Builder) metaCachePath(dsName string) string {
	return filepath.Join(b.cacheDir, "datasource", dsName+".yaml")
}

func (b *Builder) sessionPath(id string) string {
	return filepath.Join(b.cacheDir, "skill", id+".json")
}

func (b *Builder) persist(s *Session) {
	p := b.sessionPath(s.ID)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = writeUTF8(p, string(data))
}

func (b *Builder) loadFromDisk(id string) (*Session, error) {
	data, err := os.ReadFile(b.sessionPath(id))
	if err != nil {
		return nil, err
	}
	s := &Session{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}

// ============================================================
// 工具函数
// ============================================================

func newCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// yamlMarshal / yamlUnmarshal 包装 gopkg.in/yaml.v3
var yamlMarshal = func(v any) ([]byte, error) {
	return yaml.Marshal(v)
}

var yamlUnmarshal = func(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

func writeUTF8(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// buildPluginYAML 根据 session 生成 plugin YAML 字符串
// W4 简化:模板字符串,根据 Design/Measures/Dimensions 渲染
func buildPluginYAML(s *Session) string {
	d := s.Design
	var measures, dimensions, segments string
	for i, m := range d.Measures {
		measures += "        - name: " + m + "\n"
		// 简单类型推断
		if m == "count" {
			measures += "          type: count\n"
		} else {
			measures += "          type: sum\n          sql: " + m + "\n"
		}
		_ = i
	}
	for _, dn := range d.Dimensions {
		dimensions += "        - name: " + dn + "\n          sql: " + dn + "\n          type: string\n"
	}
	for _, sg := range d.Segments {
		segments += "        - name: " + sg + "\n          sql: \"{CUBE}.status IN ('paid', 'shipped', 'done')\"\n"
	}

	gen := "skill"
	if d.CubeDescription == "" {
		d.CubeDescription = "AI skill 生成"
	}

	return fmt.Sprintf(`# ============================================================
# Plugin: %s
# 用途:   %s
# 数据源: %s
# 生成:   %s by AI skill v0.1
# 业务:   %s
# 维护:   修改后 agent 会自动热加载
# ============================================================
apiVersion: cube-agent/v1
kind: Plugin
metadata:
  name: %s
  version: 0.1.0
  description: %s
  datasource: %s
  owner: ai-skill
  generated_by: %s
  tags: [ai-generated]

spec:
  cubes:
    - name: %s
      sql: "%s"
      description: %s
      primary_key: %s
      measures:
%s      dimensions:
%s      segments:
%s`,
		d.CubeName, d.CubeDescription, s.Datasource, time.Now().Format(time.RFC3339), s.Intent,
		d.CubeName, d.CubeDescription, s.Datasource, gen,
		d.CubeName, d.SQLTemplate, d.CubeDescription, d.PrimaryKey,
		indent(measures, 8), indent(dimensions, 6), indent(segments, 6),
	)
}

func indent(s string, n int) string {
	prefix := ""
	for i := 0; i < n; i++ {
		prefix += " "
	}
	out := ""
	for _, line := range splitLines(s) {
		if line == "" {
			out += "\n"
			continue
		}
		out += prefix + line + "\n"
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
