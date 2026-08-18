// Package config 集中管理 agent 的所有配置
// 加载顺序(优先级从高到低):
//   1. 操作系统环境变量(必须以 CUBE_AGENT_ 前缀,内部用 _ 代替 .)
//   2. 项目根目录的 .env 文件(godotenv 加载,默认不覆盖已有 env)
//   3. config/agent.yaml 文件(可使用 ${VAR} 占位符引用环境变量)
//   4. 代码内默认值
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config 总配置
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Log      LogConfig      `mapstructure:"log"`
	Plugins  PluginsConfig  `mapstructure:"plugins"`
	AI       AIConfig       `mapstructure:"ai"`
	Security SecurityConfig `mapstructure:"security"`
}

// ServerConfig HTTP/gRPC 服务配置
type ServerConfig struct {
	HTTPAddr           string        `mapstructure:"http_addr"`
	GRPCAddr           string        `mapstructure:"grpc_addr"`
	Timeout            time.Duration `mapstructure:"timeout"`
	ReadHeaderTimeout  time.Duration `mapstructure:"read_header_timeout"`
	ShutdownTimeout    time.Duration `mapstructure:"shutdown_timeout"`
	TLS                TLSConfig     `mapstructure:"tls"`
}

type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string   `mapstructure:"level"`
	Format   string   `mapstructure:"format"`
	Output   string   `mapstructure:"output"`
	NoColor  bool     `mapstructure:"no_color"`
	Rotation Rotation `mapstructure:"rotation"`
}

type Rotation struct {
	MaxSizeMB  int  `mapstructure:"max_size_mb"`
	MaxAgeDays int  `mapstructure:"max_age_days"`
	MaxBackups int  `mapstructure:"max_backups"`
	Compress   bool `mapstructure:"compress"`
}

// PluginsConfig plugin 加载相关
type PluginsConfig struct {
	Dir             string `mapstructure:"dir"`
	Watch           bool   `mapstructure:"watch"`
	HistoryKeep     int    `mapstructure:"history_keep"`
	AutoReload      bool   `mapstructure:"auto_reload"`
	ReloadDebounceMs int   `mapstructure:"reload_debounce_ms"`
}

// AIConfig AI skill 配置
type AIConfig struct {
	CacheDir string     `mapstructure:"cache_dir"`
	Skill    SkillConfig `mapstructure:"skill"`
	LLM      LLMConfig   `mapstructure:"llm"`
}

type SkillConfig struct {
	AutoInvalidateOnSchemaChange bool   `mapstructure:"auto_invalidate_on_schema_change"`
	RequireDryRun                bool   `mapstructure:"require_dry_run"`
	MaxCubeJoins                 int    `mapstructure:"max_cube_joins"`
	RequireSecurityFilter        bool   `mapstructure:"require_security_filter"`
	DefaultAutomation            string `mapstructure:"default_automation"` // auto | semi-auto | strict
}

type LLMConfig struct {
	Provider   string        `mapstructure:"provider"`
	APIKey     string        `mapstructure:"api_key"`
	BaseURL    string        `mapstructure:"base_url"`
	Model      string        `mapstructure:"model"`
	Timeout    time.Duration `mapstructure:"timeout"`
	MaxRetries int           `mapstructure:"max_retries"`
}

// SecurityConfig 安全相关
type SecurityConfig struct {
	DefaultTenantID string `mapstructure:"default_tenant_id"`
	AuditLog        bool   `mapstructure:"audit_log"`
	AuditLogPath    string `mapstructure:"audit_log_path"`
}

// Load 加载配置
// 流程:
//   1. 找项目根目录(从 CWD 向上找 go.mod)
//   2. 在根目录尝试加载 .env(godotenv 不覆盖已有环境变量)
//   3. 加载 config/agent.yaml(支持 ${VAR} 占位符)
//   4. 启用 viper.AutomaticEnv,允许环境变量覆盖任何字段(前缀 CUBE_AGENT_)
func Load() (*Config, error) {
	root, err := findProjectRoot()
	if err != nil {
		return nil, fmt.Errorf("find project root: %w", err)
	}

	// 1. 加载 .env
	envPath := filepath.Join(root, ".env")
	if _, statErr := os.Stat(envPath); statErr == nil {
		if err := godotenv.Load(envPath); err != nil {
			return nil, fmt.Errorf("load .env: %w", err)
		}
	}
	// godotenv 默认不覆盖已存在的环境变量
	// → 实现"读 .env,被环境变量覆盖"语义

	// 2. 加载 YAML
	v := viper.New()
	v.SetConfigName("agent")
	v.SetConfigType("yaml")
	v.AddConfigPath(filepath.Join(root, "config"))
	v.AddConfigPath(".")
	v.AddConfigPath("./config")

	// 关键: 启用环境变量展开
	// 这样 yaml 里的 ${DEEPSEEK_API_KEY} 会被解析
	v.AutomaticEnv()
	v.SetEnvPrefix("CUBE_AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.SetTypeByDefaultValue(true)

	// 显式允许 viper 从环境变量读嵌套字段
	bindEnvRecursively(v, "", Config{})

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read agent.yaml: %w", err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// 兜底: ai.llm.api_key 走 ${DEEPSEEK_API_KEY} 占位符时
	// viper 不会自动展开,需要手动 expand
	cfg.expandEnvPlaceholders()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

// findProjectRoot 从当前目录向上找 go.mod,定位项目根
func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s upward", cwd)
		}
		dir = parent
	}
}

// bindEnvRecursively 递归地把 viper key 全部绑定到 CUBE_AGENT_xxx_yyy 环境变量
// 这样环境变量能覆盖任意深度的字段
func bindEnvRecursively(v *viper.Viper, prefix string, node any) {
	// 简化:对 Config 已知的叶子字段手动 bind
	// (反射实现容易遇到 mapstructure tag 解析复杂性,这里走显式 bind)
	leaves := []string{
		"server.http_addr", "server.grpc_addr",
		"log.level", "log.format", "log.output", "log.no_color",
		"plugins.dir", "plugins.watch", "plugins.auto_reload",
		"ai.cache_dir",
		"ai.llm.provider", "ai.llm.api_key", "ai.llm.base_url", "ai.llm.model",
		"security.default_tenant_id", "security.audit_log",
	}
	for _, k := range leaves {
		_ = v.BindEnv(k)
	}
}

// expandEnvPlaceholders 处理 YAML 里 ${VAR} 占位符
// viper.Unmarshal 不自动展开,需要手动
func (c *Config) expandEnvPlaceholders() {
	if strings.HasPrefix(c.AI.LLM.APIKey, "${") {
		key := strings.TrimSuffix(strings.TrimPrefix(c.AI.LLM.APIKey, "${"), "}")
		c.AI.LLM.APIKey = os.Getenv(key)
	}
}

// validate 基础校验
func (c *Config) validate() error {
	if c.Server.HTTPAddr == "" {
		return fmt.Errorf("server.http_addr is required")
	}
	if c.Plugins.Dir == "" {
		return fmt.Errorf("plugins.dir is required")
	}
	if c.AI.Skill.DefaultAutomation != "" &&
		c.AI.Skill.DefaultAutomation != "auto" &&
		c.AI.Skill.DefaultAutomation != "semi-auto" &&
		c.AI.Skill.DefaultAutomation != "strict" {
		return fmt.Errorf("ai.skill.default_automation must be one of: auto, semi-auto, strict")
	}
	return nil
}
