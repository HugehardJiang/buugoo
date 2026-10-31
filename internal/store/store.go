package store

import (
	"context"
	"errors"
	"time"
)

// 通用错误。实现层必须把底层错误映射为以下错误（用 errors.Is 判断）。
var (
	ErrNotFound      = errors.New("记录不存在")
	ErrConflict      = errors.New("记录已存在")
	ErrInviteInvalid = errors.New("邀请码无效、已过期或已用完")
	// ErrAvatarLimit 表示今年重新生成头像的次数用完了。
	ErrAvatarLimit = errors.New("今年重新生成头像的次数用完了")
)

// TableName 固定表名，避免依赖 GORM 的复数化规则。
func (UsageDaily) TableName() string { return "usage_daily" }

// TableName 固定表名。
func (AppMeta) TableName() string { return "app_meta" }

// TableName 固定表名。
func (Wallet) TableName() string { return "wallets" }

// TableName 固定表名。
func (CostDaily) TableName() string { return "cost_dailies" }

// TableName 固定表名。
func (BillingSetting) TableName() string { return "billing_settings" }

// Store 聚合所有 Repository，屏蔽 MySQL 与 SQLite 的差异。
type Store interface {
	Users() UserRepo
	Invites() InviteRepo
	Conversations() ConversationRepo
	Models() ModelRepo
	Messages() MessageRepo
	Attachments() AttachmentRepo
	Memories() MemoryRepo
	Emotions() EmotionRepo
	Reminders() ReminderRepo
	Bindings() BindingRepo
	Usage() UsageRepo
	Jobs() JobRepo
	Meta() MetaRepo

	// Tx 在一个事务中执行 fn，fn 返回错误时回滚。
	Tx(ctx context.Context, fn func(tx Store) error) error
	Ping(ctx context.Context) error
	Close() error
	// Driver 返回 "sqlite" 或 "mysql"。
	Driver() string
}

// AttachmentRepo 是附件数据访问接口。所有查询都限定用户。
type AttachmentRepo interface {
	Create(ctx context.Context, a *Attachment) error
	Get(ctx context.Context, userID, id uint64) (*Attachment, error)
	// ListPending 返回 ids 中属于该用户且尚未发送的附件，按 ids 的顺序。
	ListPending(ctx context.Context, userID uint64, ids []uint64) ([]Attachment, error)
	// Attach 把尚未发送的附件关联到消息。
	Attach(ctx context.Context, userID uint64, ids []uint64, conversationID, messageID uint64) error
	// ListByMessages 返回这些消息的附件（按 ID 正序）。
	ListByMessages(ctx context.Context, userID uint64, messageIDs []uint64) ([]Attachment, error)
	ListByConversation(ctx context.Context, userID, conversationID uint64) ([]Attachment, error)
	// ListStale 返回 before 之前上传、一直没有发送的附件（所有用户）。
	ListStale(ctx context.Context, before time.Time, limit int) ([]Attachment, error)
	// TotalSize 返回用户所有附件（不含已删除的）的总字节数。
	TotalSize(ctx context.Context, userID uint64) (int64, error)
	// ListFiles 按上传时间倒序列出用户已经发送的、没有删除的附件（文件管理），并返回总数。kind 为空时不限类型。
	ListFiles(ctx context.Context, userID uint64, kind string, limit, offset int) ([]Attachment, int64, error)
	// ListBefore 返回用户在 before 之前上传、没有删除的全部附件。
	ListBefore(ctx context.Context, userID uint64, before time.Time) ([]Attachment, error)
	// ListByIDs 返回 ids 中属于该用户、没有删除的附件。
	ListByIDs(ctx context.Context, userID uint64, ids []uint64) ([]Attachment, error)
	// MarkDeleted 把附件标记为已删除，并清掉提取的文字（看图描述保留，供对话上下文使用）。
	MarkDeleted(ctx context.Context, userID uint64, ids []uint64, at time.Time) error
	SetDescription(ctx context.Context, id uint64, desc string) error
	// Delete 删除附件记录（文件由调用方删除）。
	Delete(ctx context.Context, ids []uint64) error
}

// EmotionRepo 是情绪状态数据访问接口。
type EmotionRepo interface {
	// Get 返回用户对应的情绪状态，不存在时返回 ErrNotFound。
	Get(ctx context.Context, userID uint64) (*EmotionState, error)
	// Save 保存情绪状态（不存在则创建）。
	Save(ctx context.Context, s *EmotionState) error
	AddEvent(ctx context.Context, e *EmotionEvent) error
	// Events 返回最近的情绪变化记录（新的在前）。
	Events(ctx context.Context, userID uint64, limit int) ([]EmotionEvent, error)
	// Reset 删除用户的情绪状态与记录。
	Reset(ctx context.Context, userID uint64) error
}

// UserRepo 是用户数据访问接口。
type UserRepo interface {
	// Create 创建用户；用户名重复时返回 ErrConflict。
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, id uint64) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	Count(ctx context.Context) (int64, error)
	List(ctx context.Context, limit, offset int) ([]User, error)
	// EnsureOwner 确保 single 模式下的唯一用户存在并返回它。
	EnsureOwner(ctx context.Context) (*User, error)
	// UpdatePassword 更新密码哈希并使旧令牌全部失效。
	UpdatePassword(ctx context.Context, id uint64, hash string) error
	// BumpTokenVersion 使该用户的全部令牌失效。
	BumpTokenVersion(ctx context.Context, id uint64) error
	SetAdmin(ctx context.Context, id uint64, admin bool) error
	// SetPersonaPreset 设置用户选的预设性格（空为默认）。
	SetPersonaPreset(ctx context.Context, id uint64, preset string) error
	// SetProactive 设置是否允许布谷主动找用户聊天。
	SetProactive(ctx context.Context, id uint64, on bool) error
	// MarkProactive 记录布谷主动发消息的时间。
	MarkProactive(ctx context.Context, id uint64, at time.Time) error
	// UpdateFields 修改用户的个人设置（键为列名，只允许 display_name、disabled_skills、disabled_plugins、agent_model_id）。
	UpdateFields(ctx context.Context, id uint64, fields map[string]any) error
	// RegenerateAvatar 换成新的头像种子，year 年里最多 limit 次（到了返回 ErrAvatarLimit），返回今年已用的次数。
	RegenerateAvatar(ctx context.Context, id uint64, seed string, year, limit int) (int, error)
}

// ReminderRepo 是提醒数据访问接口。
type ReminderRepo interface {
	Create(ctx context.Context, r *Reminder) error
	// ListPending 按时间顺序返回用户还在进行（没到期、重复中或暂停）的提醒与定时任务。
	ListPending(ctx context.Context, userID uint64) ([]Reminder, error)
	// Get 返回用户还在进行的一个提醒，不存在或已结束时返回 ErrNotFound。
	Get(ctx context.Context, userID, id uint64) (*Reminder, error)
	// Update 修改用户还在进行的提醒（键为列名），不存在或已结束时返回 ErrNotFound。
	Update(ctx context.Context, userID, id uint64, fields map[string]any) error
	// Cancel 取消用户的提醒，不存在或已结束时返回 ErrNotFound。
	Cancel(ctx context.Context, userID, id uint64) error
	// Due 返回所有用户中到期（DueAt <= now）的提醒。
	Due(ctx context.Context, now time.Time, limit int) ([]Reminder, error)
	// Fired 记录一次提醒：next 为空时结束，否则改到下次时间。
	Fired(ctx context.Context, id uint64, at time.Time, next *time.Time) error
}

// BindingRepo 是聊天软件绑定的数据访问接口。
type BindingRepo interface {
	// Create 创建绑定；这个聊天软件账号已经绑定过时返回 ErrConflict。
	Create(ctx context.Context, b *ChannelBinding) error
	GetByPeer(ctx context.Context, channel, peerID string) (*ChannelBinding, error)
	// ListByUser 按最近聊天时间倒序返回用户的绑定。
	ListByUser(ctx context.Context, userID uint64) ([]ChannelBinding, error)
	Delete(ctx context.Context, userID, id uint64) error
	// Update 更新字段（键为列名），例如 conversation_id、last_message_at、peer_name。
	Update(ctx context.Context, id uint64, fields map[string]any) error
}

// InviteRepo 是邀请码数据访问接口。
type InviteRepo interface {
	Create(ctx context.Context, inv *Invite) error
	List(ctx context.Context, limit, offset int) ([]Invite, error)
	// Redeem 原子地校验并消耗一次邀请码，不可用时返回 ErrInviteInvalid。
	Redeem(ctx context.Context, code string, now time.Time) (*Invite, error)
	Delete(ctx context.Context, id uint64) error
}

// ConversationRepo 是对话数据访问接口。
type ConversationRepo interface {
	// Primary 返回用户最近使用的对话（不含临时对话），不存在时创建（终端聊天等未指定对话的客户端使用）。
	Primary(ctx context.Context, userID uint64) (*Conversation, error)
	Create(ctx context.Context, c *Conversation) error
	Get(ctx context.Context, userID, id uint64) (*Conversation, error)
	// List 按最近活动时间倒序列出用户的对话（不含临时对话）。
	List(ctx context.Context, userID uint64) ([]Conversation, error)
	// Update 更新标题、模型、思考等级等字段（键为列名）。
	Update(ctx context.Context, userID, id uint64, fields map[string]any) error
	// Delete 删除对话及其消息（记忆不受影响）。
	Delete(ctx context.Context, userID, id uint64) error
	Touch(ctx context.Context, userID, id uint64, at time.Time) error
	// ClearModel 把使用某个模型的对话改回默认模型（删除模型时调用）。
	ClearModel(ctx context.Context, modelID uint64) error
	// StaleTemporary 列出所有用户在 before 之前就没动静的临时对话（定时清理用）。
	StaleTemporary(ctx context.Context, before time.Time) ([]Conversation, error)
	// StaleEmpty 列出 before 之前就没动过、一条消息都没有的网页对话（不含聊天软件的对话和临时对话）。
	StaleEmpty(ctx context.Context, before time.Time) ([]Conversation, error)
}

// ModelRepo 是模型配置数据访问接口。
type ModelRepo interface {
	Create(ctx context.Context, m *ModelConfig) error
	Get(ctx context.Context, id uint64) (*ModelConfig, error)
	// ListVisible 列出公共模型与该用户自己的模型。
	ListVisible(ctx context.Context, userID uint64) ([]ModelConfig, error)
	Update(ctx context.Context, m *ModelConfig) error
	Delete(ctx context.Context, id uint64) error
	// ClearDefault 取消该所有者的默认模型。
	ClearDefault(ctx context.Context, ownerID uint64) error
	// SetDefault 把 id 设为该所有者的默认模型（同一所有者只有一个默认模型）。
	SetDefault(ctx context.Context, ownerID, id uint64) error
}

// MessageRepo 是消息数据访问接口。
type MessageRepo interface {
	Create(ctx context.Context, m *Message) error
	// Finish 更新助手消息的内容、状态与用量（流式输出结束后调用）。
	Finish(ctx context.Context, m *Message) error
	// CacheStats 返回对话里缓存命中的情况：最近一条有缓存信息的回复的命中率，和全部有缓存信息的回复合计的命中率
	// （百分比，没有数据时为空）。
	CacheStats(ctx context.Context, userID, conversationID uint64) (last, total *int, err error)
	// Recent 按时间正序返回对话中最近 limit 条消息。
	Recent(ctx context.Context, userID, conversationID uint64, limit int) ([]Message, error)
	// Before 按时间正序返回 ID 小于 beforeID 的最近 limit 条消息（beforeID 为 0 表示从最新开始）。
	Before(ctx context.Context, userID, conversationID, beforeID uint64, limit int) ([]Message, error)
	// After 按时间正序返回 ID 大于 afterID 的最早 limit 条消息。
	After(ctx context.Context, userID, conversationID, afterID uint64, limit int) ([]Message, error)
	// LastUserMessage 返回用户最近发的一条消息（所有对话），没有时返回 ErrNotFound。
	LastUserMessage(ctx context.Context, userID uint64) (*Message, error)
	// LatestID 返回对话中最新一条消息的 ID（没有时为 0），用于网页检查有没有新消息。
	LatestID(ctx context.Context, userID, conversationID uint64) (uint64, error)
	// Delete 删除一条消息（定时任务没做成时删掉占位消息）。
	Delete(ctx context.Context, userID, id uint64) error
}

// MemoryFilter 是记忆列表的筛选条件。
type MemoryFilter struct {
	Type   string // 为空表示不限
	Source string // 为空表示不限
	Query  string // 内容关键字，为空表示不限
}

// EmbeddingRow 是检索时加载的向量行。
type EmbeddingRow struct {
	ID        uint64
	Embedding []byte
}

// MemoryRepo 是记忆数据访问接口。所有面向用户的方法都必须带 userID，严格隔离。
type MemoryRepo interface {
	Create(ctx context.Context, m *Memory) error
	Get(ctx context.Context, userID, id uint64) (*Memory, error)
	GetMany(ctx context.Context, userID uint64, ids []uint64) ([]Memory, error)
	List(ctx context.Context, userID uint64, f MemoryFilter, limit, offset int) ([]Memory, int64, error)
	// LoadEmbeddings 加载该用户由指定模型生成的全部未删除记忆向量。
	LoadEmbeddings(ctx context.Context, userID uint64, model string) ([]EmbeddingRow, error)
	// TouchAccessed 更新被使用记忆的访问时间、次数，并增加 boost 的强度。
	TouchAccessed(ctx context.Context, userID uint64, ids []uint64, boost float64, at time.Time) error
	// Delete 清空记忆内容与向量并标记删除（不可恢复）。不存在时返回 ErrNotFound。
	Delete(ctx context.Context, userID, id uint64) error
	RecordUsage(ctx context.Context, usages []MemoryUsage) error

	// ListForReembed 跨用户按 ID 升序列出 ID 大于 afterID、且向量不是由 model 生成的未删除记忆。
	// onlyStale 为 false 时列出全部未删除记忆。供 buugoo reembed 使用。
	ListForReembed(ctx context.Context, afterID uint64, model string, onlyStale bool, limit int) ([]Memory, error)
	UpdateEmbedding(ctx context.Context, id uint64, embedding []byte, model string, dim int) error
	// CountByModel 统计未删除记忆按向量模型的分布。
	CountByModel(ctx context.Context) (map[string]int64, error)
}

// UsageRepo 是每日用量数据访问接口。
type UsageRepo interface {
	// Get 返回某天的用量，不存在时返回零值（不报错）。
	Get(ctx context.Context, userID uint64, day string) (UsageDaily, error)
	// Add 原子地累加某天的用量。
	Add(ctx context.Context, userID uint64, day string, messages, promptTokens, completionTokens int) error
	// AddSearches 原子地累加某天的联网搜索次数。
	AddSearches(ctx context.Context, userID uint64, day string, n int) error
	// Sum 返回 [fromDay, toDay] 之间（含两头）的用量合计，Day 为空。
	Sum(ctx context.Context, userID uint64, fromDay, toDay string) (UsageDaily, error)
}

// JobRepo 是后台任务运行记录的数据访问接口。
type JobRepo interface {
	Start(ctx context.Context, name string, at time.Time) (*JobRun, error)
	Finish(ctx context.Context, id uint64, status, errMsg string, at time.Time) error
	// LastSucceeded 返回最近一次成功的运行，没有时返回 ErrNotFound。
	LastSucceeded(ctx context.Context, name string) (*JobRun, error)
}

// MetaRepo 是全局键值对数据访问接口。
type MetaRepo interface {
	// Get 返回键对应的值；不存在时 ok 为 false。
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string) error
}
