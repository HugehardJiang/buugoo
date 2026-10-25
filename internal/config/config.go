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
	"regexp"
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
	Security  SecurityConfig  `koanf:"security"`
	Uploads   UploadsConfig   `koanf:"uploads"`
	Tools     ToolsConfig     `koanf:"tools"`
	Emotion   EmotionConfig   `koanf:"emotion"`
	Admin     AdminConfig     `koanf:"admin"`

	// path 记录实际加载的配置文件路径（未加载文件时为空），不参与反序列化。
	path string
	// notices 是加载配置时自动调整的说明，启动时写入日志。
	notices []string
	// explicit 记录配置文件或环境变量中明确设置过的键。
	explicit map[string]bool
	// fromEnv 记录由环境变量设置的键（优先级最高，网页设置无法覆盖）。
	fromEnv map[string]bool
}

// SettingsFile 是网页管理后台保存设置的文件名，位于数据目录中。
const SettingsFile = "settings.yaml"

// SettingsPath 返回网页设置文件的路径。
func (c *Config) SettingsPath() string { return filepath.Join(c.DataDir, SettingsFile) }

// FromEnv 报告该键是否由环境变量设置。
func (c *Config) FromEnv(key string) bool { return c.fromEnv[key] }

// fileSets 报告配置文件或环境变量是否明确设置了该键（例如 llm.chat.vision）。
func (c *Config) fileSets(key string) bool { return c.explicit[key] }

// Notices 返回加载配置时自动调整的说明。
func (c *Config) Notices() []string { return c.notices }

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
	// ThinkingStyle 是开启思考的参数格式：none | reasoning_effort | enable_thinking | model_switch。
	ThinkingStyle string `koanf:"thinking_style"`
	// ThinkingModel 是 model_switch 方式下开启思考时改用的模型。
	ThinkingModel string `koanf:"thinking_model"`
	// Vision 表示模型能理解图片（多模态）。不能看图的模型收到图片时，先由能看图的模型转述成文字。
	Vision bool `koanf:"vision"`
	// Tools 表示模型支持工具调用（联网搜索、运行代码）。
	Tools bool `koanf:"tools"`
}

// UpgradeLegacyDeepSeek 把已停用的 deepseek-chat / deepseek-reasoner 换成 deepseek-flash，
// 并改用新的思考参数。返回是否做了替换。
func UpgradeLegacyDeepSeek(m *ModelConfig) bool {
	if !strings.Contains(m.BaseURL, "api.deepseek.com") {
		return false
	}
	switch m.Model {
	case "deepseek-chat", "deepseek-reasoner":
		m.Model, m.ThinkingStyle, m.ThinkingModel, m.Vision = "deepseek-flash", "deepseek", "", true
		return true
	}
	return false
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

// UploadsConfig 是文件上传配置。
type UploadsConfig struct {
	// Dir 是上传文件的存放目录，留空为 <data_dir>/uploads。
	Dir string `koanf:"dir"`
	// MaxFileMB 是单个文件的大小上限（MB）。
	MaxFileMB int `koanf:"max_file_mb"`
	// MaxFiles 是一条消息最多附带的文件数。
	MaxFiles int `koanf:"max_files"`
	// UserQuotaMB 是每个用户上传文件的总量上限（MB），0 表示不限制。
	UserQuotaMB int `koanf:"user_quota_mb"`
	// DocChars 是一个文档放进对话的最多字数，超出部分截断。
	DocChars int `koanf:"doc_chars"`
}

// EmotionConfig 是情绪系统的配置（布谷的脾气）。各系数以 1 为标准，越大越明显。
type EmotionConfig struct {
	Enabled bool `koanf:"enabled"`
	// Expressiveness 是情绪起伏的整体幅度。
	Expressiveness float64 `koanf:"expressiveness"`
	// Temper 是易怒程度。
	Temper float64 `koanf:"temper"`
	// Patience 是耐心，越大越不容易不耐烦。
	Patience float64 `koanf:"patience"`
	// Forgiveness 是消气的速度。
	Forgiveness float64 `koanf:"forgiveness"`
	// Empathy 是被对方情绪感染的程度。
	Empathy float64 `koanf:"empathy"`
}

// ToolsConfig 是模型可以调用的工具配置。
type ToolsConfig struct {
	Python PythonConfig `koanf:"python"`
	Search SearchConfig `koanf:"search"`
}

// 联网搜索服务。
const (
	SearchAuto     = "auto"     // 当前模型是 DeepSeek 时用 DeepSeek，否则用配置了 Key 的博查或 Tavily
	SearchDeepSeek = "deepseek" // 只在当前模型是 DeepSeek 时可用
	SearchBocha    = "bocha"
	SearchTavily   = "tavily"
	SearchOff      = "off"
)

// SearchConfig 是联网搜索与读取网页的配置。
type SearchConfig struct {
	Provider     string `koanf:"provider"`
	BochaAPIKey  string `koanf:"bocha_api_key"`
	TavilyAPIKey string `koanf:"tavily_api_key"`
	// DirectFetch 为 true 时，搜索服务不能读取网页（例如博查、或没有搜索服务）也由服务器直接下载网页读取正文（只能访问公网）。
	DirectFetch bool `koanf:"direct_fetch"`
}

// Python 运行方式。
const (
	PythonDocker = "docker" // 在一次性 Docker 容器中运行：断网、只读、限制资源（所有用户可用）
	PythonLocal  = "local"  // 用本机 python3 运行，只限制时间和内存（multi 模式下仅管理员可用）
	PythonOff    = "off"
)

// PythonConfig 是运行 Python 代码的配置。
type PythonConfig struct {
	Runner   string        `koanf:"runner"`
	Image    string        `koanf:"image"`  // docker 方式使用的镜像
	Docker   string        `koanf:"docker"` // docker 命令
	Python   string        `koanf:"python"` // local 方式使用的解释器
	Timeout  time.Duration `koanf:"timeout"`
	MemoryMB int           `koanf:"memory_mb"`
	// Packages 是预装的第三方库，启动时安装到 LibDir（已经装好的不会重复安装）。
	Packages []string `koanf:"packages"`
	// AllowInstall 允许模型按需安装 PyPI 上的库（multi 模式下仅管理员的对话）。
	AllowInstall bool `koanf:"allow_install"`
	// IndexURL 是 pip 使用的软件源，留空为官方 PyPI，例如 https://pypi.tuna.tsinghua.edu.cn/simple。
	IndexURL string `koanf:"index_url"`
	// LibDir 是第三方库的存放目录，所有人共用，留空为 <data_dir>/pylib。
	LibDir string `koanf:"lib_dir"`
	// LibMaxMB 是第三方库目录的大小上限，超过后不再安装新库。
	LibMaxMB int `koanf:"lib_max_mb"`
	// InstallTimeout 是一次安装的时间上限。
	InstallTimeout time.Duration `koanf:"install_timeout"`
}

// packageSpec 只允许「包名[extras][版本约束]」，不允许以 - 开头的 pip 参数、网址或路径。
var packageSpec = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}(\[[A-Za-z0-9._,-]{1,100}\])?((==|>=|<=|~=|!=|>|<)[A-Za-z0-9.*+!-]{1,50}(,(==|>=|<=|~=|!=|>|<)[A-Za-z0-9.*+!-]{1,50})*)?$`)

// ValidPackageSpec 报告是否为合法的 pip 包名（可带版本约束）。
func ValidPackageSpec(s string) bool { return packageSpec.MatchString(s) }

// PersonaConfig 是人格档案配置。
type PersonaConfig struct {
	File string `koanf:"file"` // 为空时使用内置默认人格
	// DisabledPresets 是关掉的预设性格 ID（gentle、snarky、strategist、bubbly），用户不能再选；已经选了的改用默认人格。
	DisabledPresets []string `koanf:"disabled_presets"`
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

// DefaultAdminPassword 是管理后台的默认密码，请在配置文件中修改。
const DefaultAdminPassword = "1228"

// AdminConfig 是网页管理后台的配置。
type AdminConfig struct {
	// Password 是管理后台的登录密码，只能在配置文件或环境变量中修改，网页上不能改。
	Password string `koanf:"password"`
}

// SecurityConfig 是加密相关配置。
type SecurityConfig struct {
	// EncryptionKey 用于加密数据库中保存的模型 API Key（64 位十六进制，即 32 字节）。
	// 留空时自动生成并保存到 <data_dir>/secret.key；建议写在配置文件或环境变量中，
	// 不与数据库放在同一处。
	EncryptionKey string `koanf:"encryption_key"`
	// AllowPrivateModelURLs 为 true 时，multi 模式下普通用户添加的模型也可以连接内网/本机地址
	// （例如局域网里的 Ollama）。默认关闭，防止借模型地址探测服务器内网（SSRF）。
	AllowPrivateModelURLs bool `koanf:"allow_private_model_urls"`
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
				Model:       "deepseek-flash",
				Temperature: 0.8,
				MaxTokens:   1024,
				Timeout:     120 * time.Second,
				// DeepSeek：thinking.type 开关 + reasoning_effort 调节思考强度；deepseek-flash 支持图片。
				ThinkingStyle: "deepseek",
				Vision:        true,
				Tools:         true,
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
		Uploads: UploadsConfig{MaxFileMB: 20, MaxFiles: 8, UserQuotaMB: 500, DocChars: 20000},
		Admin:   AdminConfig{Password: DefaultAdminPassword},
		Emotion: EmotionConfig{Enabled: true, Expressiveness: 1, Temper: 1, Patience: 1, Forgiveness: 1, Empathy: 1},
		Tools: ToolsConfig{Python: PythonConfig{
			Runner: PythonDocker, Image: "python:3.12-slim", Docker: "docker", Python: "python3",
			Timeout: 30 * time.Second, MemoryMB: 256,
			Packages:     []string{"numpy", "scipy", "sympy", "pandas", "mpmath", "matplotlib"},
			AllowInstall: true, LibMaxMB: 2048, InstallTimeout: 5 * time.Minute,
		}, Search: SearchConfig{Provider: SearchAuto, DirectFetch: true}},
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
	envK := koanf.New(".")
	if err := envK.Load(env.Provider(".", env.Opt{
		Prefix:        EnvPrefix,
		TransformFunc: envTransform,
		EnvironFunc:   environ,
	}), nil); err != nil {
		return nil, fmt.Errorf("读取环境变量: %w", err)
	}
	k := koanf.New(".")
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
		}
	}
	// 网页管理后台保存的设置在数据目录中，覆盖配置文件；环境变量优先级最高。
	dataDir := envK.String("data_dir")
	if dataDir == "" {
		dataDir = k.String("data_dir")
	}
	if dataDir == "" {
		dataDir = fallbackDataDir
	}
	if dataDir != "" {
		sp := filepath.Join(dataDir, SettingsFile)
		if _, err := os.Stat(sp); err == nil {
			if err := k.Load(file.Provider(sp), yaml.Parser()); err != nil {
				return nil, fmt.Errorf("读取网页设置 %s: %w", sp, err)
			}
		}
	}
	if err := k.Merge(envK); err != nil {
		return nil, err
	}

	cfg := Default()
	if err := k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	cfg.explicit, cfg.fromEnv = map[string]bool{}, map[string]bool{}
	for _, key := range k.Keys() {
		cfg.explicit[key] = true
	}
	for _, key := range envK.Keys() {
		cfg.fromEnv[key] = true
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
	if old := c.LLM.Chat.Model; UpgradeLegacyDeepSeek(&c.LLM.Chat) {
		c.notices = append(c.notices, fmt.Sprintf("DeepSeek 已停用旧模型名 %s，已自动改用 deepseek-flash（建议同步修改配置文件 llm.chat.model）", old))
	}
	// 默认的 DeepSeek 思考参数只适用于 DeepSeek；配置换了别家模型又没写 thinking_style 时关闭思考选项，
	// 默认开启的看图能力也一并关闭。
	if c.LLM.Chat.ThinkingStyle == "deepseek" && !strings.Contains(strings.ToLower(c.LLM.Chat.Model), "deepseek") {
		c.LLM.Chat.ThinkingStyle = "none"
	}
	if c.LLM.Chat.Vision && c.LLM.Chat.Model != "deepseek-flash" && !c.fileSets("llm.chat.vision") {
		c.LLM.Chat.Vision = false
	}
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
	if c.Tools.Python.LibDir == "" && c.DataDir != "" {
		c.Tools.Python.LibDir = filepath.Join(c.DataDir, "pylib")
	}
	if c.Uploads.Dir == "" && c.DataDir != "" {
		c.Uploads.Dir = filepath.Join(c.DataDir, "uploads")
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
	switch c.LLM.Chat.ThinkingStyle {
	case "", "none", "reasoning_effort", "enable_thinking", "model_switch", "deepseek":
	default:
		errs = append(errs, fmt.Errorf("llm.chat.thinking_style %q 无效（可选 none、reasoning_effort、enable_thinking、model_switch、deepseek）", c.LLM.Chat.ThinkingStyle))
	}
	if c.Uploads.MaxFileMB <= 0 || c.Uploads.MaxFiles <= 0 || c.Uploads.UserQuotaMB < 0 || c.Uploads.DocChars <= 0 {
		errs = append(errs, errors.New("uploads.max_file_mb、max_files、doc_chars 必须大于 0，user_quota_mb 不能小于 0"))
	}
	for name, v := range map[string]float64{"expressiveness": c.Emotion.Expressiveness, "temper": c.Emotion.Temper,
		"patience": c.Emotion.Patience, "forgiveness": c.Emotion.Forgiveness, "empathy": c.Emotion.Empathy} {
		if v < 0 || v > 3 {
			errs = append(errs, fmt.Errorf("emotion.%s 需在 0-3 之间", name))
		}
	}
	switch c.Tools.Search.Provider {
	case SearchAuto, SearchDeepSeek, SearchBocha, SearchTavily, SearchOff:
	default:
		errs = append(errs, fmt.Errorf("tools.search.provider %q 无效（可选 auto、deepseek、bocha、tavily、off）", c.Tools.Search.Provider))
	}
	if c.Tools.Search.Provider == SearchBocha && strings.TrimSpace(c.Tools.Search.BochaAPIKey) == "" {
		errs = append(errs, errors.New("tools.search.provider 为 bocha 时需要填写 tools.search.bocha_api_key"))
	}
	if c.Tools.Search.Provider == SearchTavily && strings.TrimSpace(c.Tools.Search.TavilyAPIKey) == "" {
		errs = append(errs, errors.New("tools.search.provider 为 tavily 时需要填写 tools.search.tavily_api_key"))
	}
	switch c.Tools.Python.Runner {
	case PythonDocker, PythonLocal, PythonOff:
	default:
		errs = append(errs, fmt.Errorf("tools.python.runner %q 无效（可选 docker、local、off）", c.Tools.Python.Runner))
	}
	if c.Tools.Python.Timeout <= 0 || c.Tools.Python.MemoryMB <= 0 || c.Tools.Python.InstallTimeout <= 0 || c.Tools.Python.LibMaxMB < 0 {
		errs = append(errs, errors.New("tools.python.timeout、memory_mb、install_timeout 必须大于 0，lib_max_mb 不能小于 0"))
	}
	for _, p := range c.Tools.Python.Packages {
		if !ValidPackageSpec(p) {
			errs = append(errs, fmt.Errorf("tools.python.packages 中的 %q 不是合法的包名", p))
		}
	}
	if u := c.Tools.Python.IndexURL; u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		errs = append(errs, errors.New("tools.python.index_url 需以 http:// 或 https:// 开头"))
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
