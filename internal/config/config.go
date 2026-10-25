// Package config 定义 Buugoo 的配置结构，并负责加载与校验。
//
// 加载顺序（后者覆盖前者）：内置默认值 → YAML 配置文件 → 环境变量。
// 环境变量以 BUUGOO_ 为前缀，层级之间用双下划线分隔，例如：
//
//	BUUGOO_LLM__CHAT__API_KEY=sk-xxx
//	BUUGOO_DATABASE__DSN=user:pass@tcp(127.0.0.1:3306)/buugoo
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// 部署模式。
const (
	ModeSingle = "single"
	ModeMulti  = "multi"
)

// 注册方式（仅 multi 模式有效）。
const (
	RegistrationOpen   = "open"
	RegistrationInvite = "invite"
	RegistrationClosed = "closed"
)

// 数据库驱动。
const (
	DriverSQLite = "sqlite"
	DriverMySQL  = "mysql"
)

// EnvPrefix 是环境变量覆盖的前缀。
const EnvPrefix = "BUUGOO_"

// Config 是完整的配置结构。
type Config struct {
	Mode     string `koanf:"mode"`
	DataDir  string `koanf:"data_dir"`
	Timezone string `koanf:"timezone"`

	Server    ServerConfig    `koanf:"server"`
	Auth      AuthConfig      `koanf:"auth"`
	Database  DatabaseConfig  `koanf:"database"`
	LLM       LLMConfig       `koanf:"llm"`
	Memory    MemoryConfig    `koanf:"memory"`
	Chat      ChatConfig      `koanf:"chat"`
	Persona   PersonaConfig   `koanf:"persona"`
	Quota     QuotaConfig     `koanf:"quota"`
	Scheduler SchedulerConfig `koanf:"scheduler"`
	Update    UpdateConfig    `koanf:"update"`
	Log       LogConfig       `koanf:"log"`
	Debug     DebugConfig     `koanf:"debug"`

	// path 记录实际加载的配置文件路径（未加载文件时为空），不参与反序列化。
	path string
}

// ServerConfig 是 HTTP 服务配置。
type ServerConfig struct {
	Listen       string        `koanf:"listen"`
	BaseURL      string        `koanf:"base_url"`
	CORSOrigins  []string      `koanf:"cors_origins"`
	ReadTimeout  time.Duration `koanf:"read_timeout"`
	WriteTimeout time.Duration `koanf:"write_timeout"` // 0 表示不限制（SSE 需要长连接）
}

// AuthConfig 是鉴权配置。
type AuthConfig struct {
	// AccessPassword 仅在 single 模式下使用；为空表示无需密码。
	AccessPassword string        `koanf:"access_password"`
	JWTSecret      string        `koanf:"jwt_secret"`
	TokenTTL       time.Duration `koanf:"token_ttl"`
	// Registration 仅在 multi 模式下使用：open | invite | closed。
	Registration string `koanf:"registration"`
}

// DatabaseConfig 是数据库配置。
type DatabaseConfig struct {
	Driver       string `koanf:"driver"`
	DSN          string `koanf:"dsn"` // sqlite 为空时使用 <data_dir>/buugoo.db
	MaxOpenConns int    `koanf:"max_open_conns"`
	MaxIdleConns int    `koanf:"max_idle_conns"`
	LogSQL       bool   `koanf:"log_sql"`
}

// LLMConfig 是大模型相关配置。
type LLMConfig struct {
	Chat      ModelConfig     `koanf:"chat"`
	Utility   ModelConfig     `koanf:"utility"` // 记忆提取等后台任务用，留空字段沿用 chat
	Embedding EmbeddingConfig `koanf:"embedding"`
}

// ModelConfig 是一个聊天补全模型的配置。
type ModelConfig struct {
	Provider    string        `koanf:"provider"` // openai（OpenAI 兼容协议）| anthropic（预留）
	BaseURL     string        `koanf:"base_url"`
	APIKey      string        `koanf:"api_key"`
	Model       string        `koanf:"model"`
	Temperature float32       `koanf:"temperature"`
	MaxTokens   int           `koanf:"max_tokens"`
	Timeout     time.Duration `koanf:"timeout"`
}

// EmbeddingConfig 是向量模型配置。
type EmbeddingConfig struct {
	Provider  string        `koanf:"provider"`
	BaseURL   string        `koanf:"base_url"`
	APIKey    string        `koanf:"api_key"`
	Model     string        `koanf:"model"`
	Dim       int           `koanf:"dim"` // 0 表示不向服务端指定维度
	BatchSize int           `koanf:"batch_size"`
	Timeout   time.Duration `koanf:"timeout"`
}

// MemoryConfig 是记忆系统配置。
type MemoryConfig struct {
	Retrieval RetrievalConfig   `koanf:"retrieval"`
	Write     MemoryWriteConfig `koanf:"write"`
	Cache     VectorCacheConfig `koanf:"cache"`
}

// RetrievalConfig 是检索打分参数：score = α·recency + β·importance + γ·relevance。
type RetrievalConfig struct {
	Alpha        float64 `koanf:"alpha"`
	Beta         float64 `koanf:"beta"`
	Gamma        float64 `koanf:"gamma"`
	Decay        float64 `koanf:"decay"` // recency = decay^小时数
	CandidateK   int     `koanf:"candidate_k"`
	TopK         int     `koanf:"top_k"`
	MinRelevance float64 `koanf:"min_relevance"`
}

// MemoryWriteConfig 是记忆写入参数。
type MemoryWriteConfig struct {
	Enabled             bool    `koanf:"enabled"`
	ImportanceThreshold int     `koanf:"importance_threshold"`
	AccessBoost         float64 `koanf:"access_boost"`
	DedupSimilarity     float64 `koanf:"dedup_similarity"`
	Workers             int     `koanf:"workers"`
	QueueSize           int     `koanf:"queue_size"`
}

// VectorCacheConfig 是内存向量缓存参数。
type VectorCacheConfig struct {
	MaxUsers int           `koanf:"max_users"`
	MaxBytes int64         `koanf:"max_bytes"`
	TTL      time.Duration `koanf:"ttl"`
}

// ChatConfig 是对话编排参数。
type ChatConfig struct {
	HistoryMessages  int `koanf:"history_messages"`   // 放进提示词的最近消息条数
	MaxContextTokens int `koanf:"max_context_tokens"` // 提示词的近似 token 预算
}

// PersonaConfig 是人格档案配置。
type PersonaConfig struct {
	File string `koanf:"file"` // 为空时使用内置默认人格
}

// QuotaConfig 是每个用户的每日用量限额，0 表示不限制。
type QuotaConfig struct {
	DailyMessages int  `koanf:"daily_messages"`
	DailyTokens   int  `koanf:"daily_tokens"`
	ExemptAdmins  bool `koanf:"exempt_admins"`
}

// SchedulerConfig 是后台调度器配置。
type SchedulerConfig struct {
	Enabled           bool   `koanf:"enabled"`
	ConsolidationSpec string `koanf:"consolidation_spec"`
}

// UpdateConfig 是更新检查配置。
type UpdateConfig struct {
	CheckOnStart bool   `koanf:"check_on_start"`
	Repo         string `koanf:"repo"`
	Mirror       string `koanf:"mirror"`
}

// LogConfig 是日志配置。
type LogConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// DebugConfig 是调试开关。
type DebugConfig struct {
	// PromptEndpoint 为 true 时开放 /api/v1/debug/prompt（single 模式下默认可用）。
	PromptEndpoint bool `koanf:"prompt_endpoint"`
}

// Path 返回实际加载的配置文件路径，未从文件加载时为空。
func (c *Config) Path() string { return c.path }

// Default 返回内置默认配置。
func Default() *Config {
	return &Config{
		Mode:     ModeSingle,
		Timezone: "Asia/Shanghai",
		Server: ServerConfig{
			Listen:      "127.0.0.1:8080",
			ReadTimeout: 30 * time.Second,
		},
		Auth: AuthConfig{
			TokenTTL:     30 * 24 * time.Hour,
			Registration: RegistrationInvite,
		},
		Database: DatabaseConfig{
			Driver:       DriverSQLite,
			MaxOpenConns: 10,
			MaxIdleConns: 5,
		},
		LLM: LLMConfig{
			Chat: ModelConfig{
				Provider:    "openai",
				BaseURL:     "https://api.deepseek.com/v1",
				Model:       "deepseek-chat",
				Temperature: 0.8,
				MaxTokens:   1024,
				Timeout:     120 * time.Second,
			},
			Utility: ModelConfig{
				Temperature: 0.2,
				MaxTokens:   1024,
			},
			Embedding: EmbeddingConfig{
				Provider:  "openai",
				BaseURL:   "https://api.siliconflow.cn/v1",
				Model:     "BAAI/bge-m3",
				BatchSize: 32,
				Timeout:   60 * time.Second,
			},
		},
		Memory: MemoryConfig{
			Retrieval: RetrievalConfig{
				Alpha:        1.0,
				Beta:         1.0,
				Gamma:        1.5,
				Decay:        0.995,
				CandidateK:   100,
				TopK:         8,
				MinRelevance: 0.3,
			},
			Write: MemoryWriteConfig{
				Enabled:             true,
				ImportanceThreshold: 4,
				AccessBoost:         0.2,
				DedupSimilarity:     0.95,
				Workers:             2,
				QueueSize:           256,
			},
			Cache: VectorCacheConfig{
				MaxUsers: 1000,
				MaxBytes: 512 << 20,
				TTL:      30 * time.Minute,
			},
		},
		Chat: ChatConfig{
			HistoryMessages:  20,
			MaxContextTokens: 6000,
		},
		Quota: QuotaConfig{ExemptAdmins: true},
		Scheduler: SchedulerConfig{
			Enabled:           true,
			ConsolidationSpec: "0 4 * * *",
		},
		Update: UpdateConfig{
			CheckOnStart: true,
			Repo:         "HugehardJiang/buugoo",
		},
		Log: LogConfig{Level: "info", Format: "text"},
	}
}

// Load 依次加载默认值、配置文件（path 为空则跳过）和环境变量，并校验结果。
// 若最终未设置 data_dir，则使用 fallbackDataDir。
func Load(path, fallbackDataDir string) (*Config, error) {
	return load(path, fallbackDataDir, os.Environ)
}

func load(path, fallbackDataDir string, environ func() []string) (*Config, error) {
	k := koanf.New(".")
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
		}
	}
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix:        EnvPrefix,
		TransformFunc: envTransform,
		EnvironFunc:   environ,
	}), nil); err != nil {
		return nil, fmt.Errorf("读取环境变量: %w", err)
	}

	cfg := Default()
	if err := k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("解析配置文件路径: %w", err)
		}
		cfg.path = abs
	}
	if cfg.DataDir == "" {
		cfg.DataDir = fallbackDataDir
	}
	cfg.applyDerived()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// envTransform 把 BUUGOO_LLM__CHAT__API_KEY 转换为 llm.chat.api_key。
// BUUGOO_CONFIG 用于指定配置文件，不作为配置项。
func envTransform(k, v string) (string, any) {
	key := strings.TrimPrefix(k, EnvPrefix)
	if key == "CONFIG" || key == "MIRROR" {
		return "", nil
	}
	key = strings.ToLower(strings.ReplaceAll(key, "__", "."))
	if key == "server.cors_origins" {
		return key, splitList(v)
	}
	return key, v
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// applyDerived 填充依赖其它字段的默认值。
func (c *Config) applyDerived() {
	u, ch := &c.LLM.Utility, c.LLM.Chat
	if u.Provider == "" {
		u.Provider = ch.Provider
	}
	if u.BaseURL == "" {
		u.BaseURL = ch.BaseURL
	}
	if u.APIKey == "" {
		u.APIKey = ch.APIKey
	}
	if u.Model == "" {
		u.Model = ch.Model
	}
	if u.Timeout == 0 {
		u.Timeout = ch.Timeout
	}
	if c.Database.Driver == DriverSQLite && c.Database.DSN == "" && c.DataDir != "" {
		c.Database.DSN = filepath.Join(c.DataDir, "buugoo.db")
	}
}

// Validate 校验配置的合法性。缺少 LLM API Key 不视为错误（允许先启动再配置），
// 由调用方在需要时提示。
func (c *Config) Validate() error {
	var errs []error
	switch c.Mode {
	case ModeSingle, ModeMulti:
	default:
		errs = append(errs, fmt.Errorf("mode 必须是 single 或 multi，当前为 %q", c.Mode))
	}
	switch c.Auth.Registration {
	case RegistrationOpen, RegistrationInvite, RegistrationClosed:
	default:
		errs = append(errs, fmt.Errorf("auth.registration 必须是 open、invite 或 closed，当前为 %q", c.Auth.Registration))
	}
	switch c.Database.Driver {
	case DriverSQLite:
		if c.Database.DSN == "" {
			errs = append(errs, errors.New("sqlite 需要设置 database.dsn 或 data_dir"))
		}
	case DriverMySQL:
		if c.Database.DSN == "" {
			errs = append(errs, errors.New("mysql 需要设置 database.dsn"))
		}
	default:
		errs = append(errs, fmt.Errorf("database.driver 必须是 sqlite 或 mysql，当前为 %q", c.Database.Driver))
	}
	if c.Mode == ModeMulti && len(c.Auth.JWTSecret) < 32 {
		errs = append(errs, errors.New("multi 模式下 auth.jwt_secret 至少 32 个字符（可用 buugoo init 生成）"))
	}
	if c.Auth.TokenTTL <= 0 {
		errs = append(errs, errors.New("auth.token_ttl 必须大于 0"))
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("timezone %q 无效: %w", c.Timezone, err))
	}
	r := c.Memory.Retrieval
	if r.Decay <= 0 || r.Decay > 1 {
		errs = append(errs, errors.New("memory.retrieval.decay 必须在 (0, 1] 之间"))
	}
	if r.Alpha < 0 || r.Beta < 0 || r.Gamma < 0 {
		errs = append(errs, errors.New("memory.retrieval 的 alpha/beta/gamma 不能为负数"))
	}
	if r.TopK <= 0 || r.CandidateK < r.TopK {
		errs = append(errs, errors.New("memory.retrieval 需要满足 0 < top_k <= candidate_k"))
	}
	w := c.Memory.Write
	if w.ImportanceThreshold < 1 || w.ImportanceThreshold > 10 {
		errs = append(errs, errors.New("memory.write.importance_threshold 必须在 1-10 之间"))
	}
	if w.Workers <= 0 || w.QueueSize <= 0 {
		errs = append(errs, errors.New("memory.write 的 workers 与 queue_size 必须大于 0"))
	}
	if c.Chat.HistoryMessages <= 0 {
		errs = append(errs, errors.New("chat.history_messages 必须大于 0"))
	}
	if c.Quota.DailyMessages < 0 || c.Quota.DailyTokens < 0 {
		errs = append(errs, errors.New("quota 不能为负数"))
	}
	for _, p := range []string{c.LLM.Chat.Provider, c.LLM.Utility.Provider} {
		if p != "openai" && p != "anthropic" {
			errs = append(errs, fmt.Errorf("不支持的 LLM provider %q", p))
		}
	}
	if c.LLM.Embedding.Provider != "openai" {
		errs = append(errs, fmt.Errorf("不支持的 embedding provider %q", c.LLM.Embedding.Provider))
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("log.level %q 无效", c.Log.Level))
	}
	return errors.Join(errs...)
}

// Location 返回配置的时区。Validate 已保证其有效。
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}
