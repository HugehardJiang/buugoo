// Package store 定义领域数据模型与数据访问接口（Repository），
// 具体实现见 gormstore 子包，同时支持 MySQL 与 SQLite。
package store

import (
	"time"

	"gorm.io/gorm"
)

// User 是用户。single 模式下只有一个 ID 为 OwnerID 的用户。
type User struct {
	ID           uint64 `gorm:"primaryKey"`
	Username     string `gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string `gorm:"size:255;not null;default:''"`
	DisplayName  string `gorm:"size:64;not null;default:''"`
	IsAdmin      bool   `gorm:"not null;default:false"`
	// TokenVersion 每次修改密码或"退出所有设备"时加一，使旧的 JWT 全部失效。
	TokenVersion int     `gorm:"not null;default:0"`
	InviteID     *uint64 `gorm:"index"`
	// PersonaPreset 是用户为布谷选的预设性格，空为默认（管理后台设置的人格）。
	PersonaPreset string `gorm:"size:32;not null;default:''"`
	// Proactive 为 true 时布谷可以主动找用户聊天（提醒不受影响）；LastProactiveAt 是上次主动发消息的时间。
	Proactive       bool `gorm:"not null;default:true"`
	LastProactiveAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// OwnerID 是 single 模式下唯一用户的 ID。
const OwnerID uint64 = 1

// OwnerUsername 是 single 模式下唯一用户的用户名。
const OwnerUsername = "owner"

// Invite 是邀请码。
type Invite struct {
	ID        uint64     `gorm:"primaryKey"`
	Code      string     `gorm:"size:32;uniqueIndex;not null"`
	CreatedBy uint64     `gorm:"index;not null"`
	MaxUses   int        `gorm:"not null;default:1"` // 0 表示不限次数
	UsedCount int        `gorm:"not null;default:0"`
	ExpiresAt *time.Time ``
	Note      string     `gorm:"size:255;not null;default:''"`
	CreatedAt time.Time
}

// Usable 报告邀请码在 now 时刻是否仍可使用。
func (i *Invite) Usable(now time.Time) bool {
	if i.ExpiresAt != nil && !now.Before(*i.ExpiresAt) {
		return false
	}
	return i.MaxUses == 0 || i.UsedCount < i.MaxUses
}

// 思考等级。
const (
	ThinkingOff    = "off"
	ThinkingLow    = "low"
	ThinkingMedium = "medium"
	ThinkingHigh   = "high"
)

// ValidThinking 报告思考等级是否合法。
func ValidThinking(t string) bool {
	switch t {
	case ThinkingOff, ThinkingLow, ThinkingMedium, ThinkingHigh:
		return true
	}
	return false
}

// Conversation 是一段对话。每个用户可以有多段对话，记忆在所有对话之间共享。
type Conversation struct {
	ID     uint64 `gorm:"primaryKey"`
	UserID uint64 `gorm:"index:idx_conv_user_last,priority:1;not null"`
	Title  string `gorm:"size:255;not null;default:''"`
	// ModelID 是这段对话使用的模型（model_configs.id），为空表示使用默认模型。
	ModelID *uint64 ``
	// Thinking 是思考等级：off | low | medium | high。
	Thinking      string `gorm:"size:16;not null;default:'off'"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastMessageAt *time.Time `gorm:"index:idx_conv_user_last,priority:2"`
	// Summary 是较早消息的摘要，SummaryUntil 是已经整理进摘要的最后一条消息 ID（之后的消息原样放进提示词）。
	Summary      string `gorm:"size:4000"`
	SummaryUntil uint64 `gorm:"not null;default:0"`
}

// 消息角色。
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// 消息状态。
const (
	MessageComplete    = "complete"
	MessageInterrupted = "interrupted" // 流式输出被客户端中断，保存了已生成部分
	MessageError       = "error"
)

// Message 是对话中的一条消息。
type Message struct {
	ID               uint64 `gorm:"primaryKey"`
	ConversationID   uint64 `gorm:"index:idx_msg_conv,priority:1;not null"`
	UserID           uint64 `gorm:"index;not null"`
	Role             string `gorm:"size:16;not null"`
	Content          string `gorm:"size:65536;not null"`
	Model            string `gorm:"size:128;not null;default:''"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	Status           string `gorm:"size:16;not null;default:'complete'"`
	// Reasoning 是模型的思考过程，用于展示；DeepSeek 使用工具时按要求回传，其他情况不放进之后的提示词。
	// 不参与记忆提取。
	Reasoning string  `gorm:"size:65536"`
	ModelID   *uint64 ``
	// ToolLog 是本轮回复中的工具调用记录（JSON 数组，见 chat.ToolRecord），用于展示。
	ToolLog   string `gorm:"size:65536"`
	CreatedAt time.Time
}

// 思考参数格式：不同服务开启思考的方式不同。
const (
	ThinkingStyleNone      = "none"             // 不支持
	ThinkingStyleEffort    = "reasoning_effort" // OpenAI 风格：reasoning_effort = low | medium | high
	ThinkingStyleEnable    = "enable_thinking"  // 通义千问 / 硅基流动风格：enable_thinking + thinking_budget
	ThinkingStyleSwitchMod = "model_switch"     // 换用单独的推理模型（旧版 DeepSeek 的 deepseek-reasoner）
	ThinkingStyleDeepSeek  = "deepseek"         // DeepSeek：thinking.type + reasoning_effort = low | high | max
)

// ValidThinkingStyle 报告思考参数格式是否合法。
func ValidThinkingStyle(s string) bool {
	switch s {
	case ThinkingStyleNone, ThinkingStyleEffort, ThinkingStyleEnable, ThinkingStyleSwitchMod, ThinkingStyleDeepSeek:
		return true
	}
	return false
}

// PublicOwner 是公共模型的 OwnerID（由管理员添加，所有用户可用）。
const PublicOwner uint64 = 0

// ModelConfig 是在网页中添加的聊天模型（OpenAI 兼容协议）。APIKey 保存的是加密后的值。
type ModelConfig struct {
	ID            uint64  `gorm:"primaryKey"`
	OwnerID       uint64  `gorm:"index;not null;default:0"`
	Name          string  `gorm:"size:64;not null"`
	BaseURL       string  `gorm:"size:512;not null"`
	APIKey        string  `gorm:"column:api_key_enc;size:2048;not null;default:''"`
	Model         string  `gorm:"size:128;not null"`
	ThinkingStyle string  `gorm:"size:32;not null;default:'none'"`
	ThinkingModel string  `gorm:"size:128;not null;default:''"`
	Temperature   float64 `gorm:"not null;default:0.8"`
	MaxTokens     int     `gorm:"not null;default:1024"`
	// Vision 表示模型能理解图片。
	Vision bool `gorm:"not null;default:false"`
	// Tools 表示模型支持工具调用（联网搜索、运行代码）。
	Tools     bool `gorm:"not null;default:true"`
	IsDefault bool `gorm:"not null;default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Public 报告是否为公共模型。
func (m *ModelConfig) Public() bool { return m.OwnerID == PublicOwner }

// 附件类型。
const (
	AttachmentImage    = "image"
	AttachmentDocument = "document"
)

// Attachment 是聊天中上传的文件。上传后先处于未发送状态（MessageID 为空），随消息发送后关联到用户消息。
// 文件本身存放在上传目录中，Path 是相对路径。
type Attachment struct {
	ID             uint64  `gorm:"primaryKey"`
	UserID         uint64  `gorm:"index;not null"`
	ConversationID *uint64 `gorm:"index"`
	MessageID      *uint64 `gorm:"index"`
	Kind           string  `gorm:"size:16;not null"`
	Filename       string  `gorm:"size:255;not null"`
	MIME           string  `gorm:"column:mime;size:128;not null"`
	Size           int64   `gorm:"not null"`
	Path           string  `gorm:"size:512;not null"`
	SHA256         string  `gorm:"column:sha256;size:64;not null"`
	// Text 是从文档中提取的文字（图片为空）。
	Text string `gorm:"size:16777215"`
	// Description 是看图模型对图片的文字描述，供不能看图的模型和记忆提取使用；生成后缓存。
	Description string `gorm:"size:8192"`
	CreatedAt   time.Time
	// Source 是附件的来源：用户上传（upload）或代码生成（generated）。
	Source string `gorm:"size:16;not null;default:''"`
	// DeletedAt 是在文件管理中删除的时间：文件已经删掉、不再计入用量，记录保留用于在聊天记录中显示「文件已删除」。
	DeletedAt *time.Time `gorm:"index"`
}

// Reminder 是用户让布谷设的提醒。
type Reminder struct {
	ID      uint64 `gorm:"primaryKey"`
	UserID  uint64 `gorm:"index;not null"`
	Content string `gorm:"size:512;not null"`
	// DueAt 是下次提醒的时间（UTC 保存）。
	DueAt time.Time `gorm:"index:idx_reminder_due,priority:2;not null"`
	// Repeat 是重复方式：空（一次）、daily、weekdays、weekly。
	Repeat string `gorm:"size:16;not null;default:''"`
	Status string `gorm:"size:16;index:idx_reminder_due,priority:1;not null;default:'pending'"`
	// FiredAt 是最近一次提醒的时间。
	FiredAt   *time.Time
	CreatedAt time.Time
}

// 提醒状态与重复方式。
const (
	ReminderPending  = "pending"
	ReminderDone     = "done"
	ReminderCanceled = "canceled"

	RepeatNone     = ""
	RepeatDaily    = "daily"
	RepeatWeekdays = "weekdays"
	RepeatWeekly   = "weekly"
)

// ChannelBinding 把聊天软件（微信、QQ、Telegram）里的一个人绑定到布谷的账号。
type ChannelBinding struct {
	ID       uint64 `gorm:"primaryKey"`
	UserID   uint64 `gorm:"index;not null"`
	Channel  string `gorm:"size:16;not null;uniqueIndex:idx_binding_peer,priority:1"`
	PeerID   string `gorm:"size:128;not null;uniqueIndex:idx_binding_peer,priority:2"`
	PeerName string `gorm:"size:128;not null;default:''"`
	// ConversationID 是在这个聊天软件里聊天使用的对话（删除后自动新建）。
	ConversationID *uint64
	CreatedAt      time.Time
	LastMessageAt  *time.Time
}

// 附件来源。
const (
	AttachmentUploaded  = "upload"
	AttachmentGenerated = "generated"
)

// EmotionState 是布谷对某个用户的情绪状态，每个用户各自一份。各情绪强度为 0-1，Mood 为 -1-1。
type EmotionState struct {
	UserID     uint64 `gorm:"primaryKey;autoIncrement:false"`
	Joy        float64
	Sadness    float64
	Anger      float64
	Worry      float64
	Surprise   float64
	Impatience float64
	Longing    float64
	// Mood 是缓慢变化的心境（负为低落，正为愉快）。
	Mood float64
	// Affinity 是亲近程度，Trust 是信任程度，都随相处缓慢变化。
	Affinity float64
	Trust    float64
	// Cause 是最近一次明显情绪变化的原因（布谷视角）。
	Cause string `gorm:"size:255"`
	// Pace 记录最近几条消息的时间与摘要（JSON），用于判断催促与重复提问。
	Pace          string `gorm:"size:4096"`
	LastMessageAt *time.Time
	// UpdatedAt 是情绪最后一次计算的时间（由情绪引擎维护，不让 GORM 自动改写）。
	UpdatedAt time.Time `gorm:"autoUpdateTime:false"`
}

// EmotionEvent 是一次情绪变化的记录，用于在「大脑皮层」中展示。
type EmotionEvent struct {
	ID     uint64 `gorm:"primaryKey"`
	UserID uint64 `gorm:"index:idx_emo_user,priority:1;not null"`
	Kind   string `gorm:"size:16;not null"` // message | absence | pace
	Cause  string `gorm:"size:255"`
	// Changes 是各情绪的变化量（JSON 对象）。
	Changes   string    `gorm:"size:1024"`
	CreatedAt time.Time `gorm:"index:idx_emo_user,priority:2"`
}

// 记忆类型。
const (
	MemoryEpisodic = "episodic" // 情景记忆：具体发生的事
	MemorySemantic = "semantic" // 语义记忆：提炼出的事实
)

// 记忆来源。
const (
	SourceStated   = "stated"   // 用户亲口说的
	SourceInferred = "inferred" // AI 推断的
)

// Memory 是一条记忆。
//
// 用户删除记忆时立即清空 Content 与 Embedding，只保留 ID、时间戳和删除标记，不可恢复。
type Memory struct {
	ID     uint64 `gorm:"primaryKey"`
	UserID uint64 `gorm:"index:idx_mem_user_del,priority:1;index:idx_mem_user_type,priority:1;index:idx_mem_user_model,priority:1;not null"`
	Type   string `gorm:"size:16;index:idx_mem_user_type,priority:2;not null"`
	// Content 是记忆文本。
	Content string `gorm:"size:65536;not null"`
	// Embedding 是归一化后的 float32 小端序向量，见 vector.Encode。
	Embedding      []byte `gorm:"size:65536"`
	EmbeddingModel string `gorm:"size:128;index:idx_mem_user_model,priority:2;not null;default:''"`
	EmbeddingDim   int    `gorm:"not null;default:0"`
	// Importance 为 1-10，写入时由 LLM 打分。
	Importance int `gorm:"not null;default:5"`
	// Emotion 为情绪强度 0-1。
	Emotion float64 `gorm:"not null;default:0"`
	// Strength 是记忆强度（遗忘曲线中的 S），每次被检索使用时增强。
	Strength float64 `gorm:"not null;default:1"`
	// Source 区分用户亲口所说与 AI 推断，防止记忆失真。
	Source          string  `gorm:"size:16;not null"`
	SourceMessageID *uint64 `gorm:"index"`
	ConversationID  *uint64 ``
	// SupersededBy 指向更新后的记忆（第二阶段：记忆冲突更新）。
	SupersededBy *uint64 `gorm:"index"`
	// ConsolidatedAt 是最近一次被巩固的时间（第二阶段：每日巩固）。
	ConsolidatedAt *time.Time ``
	// Metadata 是扩展字段（JSON）。
	Metadata       string `gorm:"size:65536;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastAccessedAt *time.Time     ``
	AccessCount    int            `gorm:"not null;default:0"`
	DeletedAt      gorm.DeletedAt `gorm:"index:idx_mem_user_del,priority:2"`
}

// MemoryUsage 记录某条回复用到了哪些记忆，便于调试与后续巩固。
type MemoryUsage struct {
	ID        uint64  `gorm:"primaryKey"`
	MessageID uint64  `gorm:"index;not null"`
	MemoryID  uint64  `gorm:"index;not null"`
	UserID    uint64  `gorm:"index;not null"`
	Score     float64 `gorm:"not null;default:0"`
	CreatedAt time.Time
}

// UsageDaily 是每个用户每天的用量，用于限额。Day 为配置时区下的 YYYY-MM-DD。
type UsageDaily struct {
	ID               uint64 `gorm:"primaryKey"`
	UserID           uint64 `gorm:"uniqueIndex:idx_usage_user_day,priority:1;not null"`
	Day              string `gorm:"size:10;uniqueIndex:idx_usage_user_day,priority:2;not null"`
	Messages         int    `gorm:"not null;default:0"`
	PromptTokens     int    `gorm:"not null;default:0"`
	CompletionTokens int    `gorm:"not null;default:0"`
	UpdatedAt        time.Time
}

// TotalTokens 返回当日总 token 数。
func (u UsageDaily) TotalTokens() int { return u.PromptTokens + u.CompletionTokens }

// 任务运行状态。
const (
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// JobRun 是一次后台任务运行记录。
type JobRun struct {
	ID         uint64    `gorm:"primaryKey"`
	JobName    string    `gorm:"size:64;index:idx_job_name_start,priority:1;not null"`
	StartedAt  time.Time `gorm:"index:idx_job_name_start,priority:2;not null"`
	FinishedAt *time.Time
	Status     string `gorm:"size:16;not null"`
	Error      string `gorm:"size:4096;not null;default:''"`
}

// AppMeta 是全局键值对（实例 ID、上次检查更新时间等）。
type AppMeta struct {
	Key       string `gorm:"column:meta_key;primaryKey;size:64"`
	Value     string `gorm:"size:4096;not null"`
	UpdatedAt time.Time
}
