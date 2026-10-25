package gormstore

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// ---------- users ----------

type userRepo struct{ db *gorm.DB }

func (r userRepo) Create(ctx context.Context, u *store.User) error {
	return mapErr(r.db.WithContext(ctx).Create(u).Error)
}

func (r userRepo) GetByID(ctx context.Context, id uint64) (*store.User, error) {
	var u store.User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		return nil, mapErr(err)
	}
	return &u, nil
}

func (r userRepo) GetByUsername(ctx context.Context, username string) (*store.User, error) {
	var u store.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, mapErr(err)
	}
	return &u, nil
}

func (r userRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&store.User{}).Count(&n).Error
	return n, mapErr(err)
}

func (r userRepo) List(ctx context.Context, limit, offset int) ([]store.User, error) {
	var us []store.User
	err := r.db.WithContext(ctx).Order("id").Limit(limit).Offset(offset).Find(&us).Error
	return us, mapErr(err)
}

func (r userRepo) EnsureOwner(ctx context.Context) (*store.User, error) {
	u, err := r.GetByID(ctx, store.OwnerID)
	if err == nil {
		return u, nil
	}
	if err != store.ErrNotFound {
		return nil, err
	}
	owner := &store.User{ID: store.OwnerID, Username: store.OwnerUsername, DisplayName: "", IsAdmin: true}
	// 并发启动时可能重复创建，忽略主键冲突后重新读取。
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(owner).Error; err != nil {
		return nil, mapErr(err)
	}
	return r.GetByID(ctx, store.OwnerID)
}

func (r userRepo) UpdatePassword(ctx context.Context, id uint64, hash string) error {
	return r.update(ctx, id, map[string]any{
		"password_hash": hash,
		"token_version": gorm.Expr("token_version + 1"),
	})
}

func (r userRepo) BumpTokenVersion(ctx context.Context, id uint64) error {
	return r.update(ctx, id, map[string]any{"token_version": gorm.Expr("token_version + 1")})
}

func (r userRepo) SetAdmin(ctx context.Context, id uint64, admin bool) error {
	return r.update(ctx, id, map[string]any{"is_admin": admin})
}

func (r userRepo) SetPersonaPreset(ctx context.Context, id uint64, preset string) error {
	return r.update(ctx, id, map[string]any{"persona_preset": preset})
}

func (r userRepo) SetProactive(ctx context.Context, id uint64, on bool) error {
	return r.update(ctx, id, map[string]any{"proactive": on})
}

func (r userRepo) MarkProactive(ctx context.Context, id uint64, at time.Time) error {
	return r.update(ctx, id, map[string]any{"last_proactive_at": at})
}

func (r userRepo) update(ctx context.Context, id uint64, values map[string]any) error {
	res := r.db.WithContext(ctx).Model(&store.User{}).Where("id = ?", id).Updates(values)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ---------- invites ----------

type inviteRepo struct{ db *gorm.DB }

func (r inviteRepo) Create(ctx context.Context, inv *store.Invite) error {
	return mapErr(r.db.WithContext(ctx).Create(inv).Error)
}

func (r inviteRepo) List(ctx context.Context, limit, offset int) ([]store.Invite, error) {
	var out []store.Invite
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Offset(offset).Find(&out).Error
	return out, mapErr(err)
}

func (r inviteRepo) Redeem(ctx context.Context, code string, now time.Time) (*store.Invite, error) {
	// 用一条带条件的 UPDATE 原子地校验并消耗，避免并发注册超出次数。
	res := r.db.WithContext(ctx).Model(&store.Invite{}).
		Where("code = ? AND (max_uses = 0 OR used_count < max_uses) AND (expires_at IS NULL OR expires_at > ?)", code, now).
		Update("used_count", gorm.Expr("used_count + 1"))
	if res.Error != nil {
		return nil, mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, store.ErrInviteInvalid
	}
	var inv store.Invite
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&inv).Error; err != nil {
		return nil, mapErr(err)
	}
	return &inv, nil
}

func (r inviteRepo) Delete(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Delete(&store.Invite{}, id)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ---------- conversations ----------

type conversationRepo struct{ db *gorm.DB }

func (r conversationRepo) Primary(ctx context.Context, userID uint64) (*store.Conversation, error) {
	var c store.Conversation
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("updated_at DESC, id DESC").First(&c).Error
	if err == nil {
		return &c, nil
	}
	if mapErr(err) != store.ErrNotFound {
		return nil, mapErr(err)
	}
	c = store.Conversation{UserID: userID, Thinking: store.ThinkingOff}
	if err := r.db.WithContext(ctx).Create(&c).Error; err != nil {
		return nil, mapErr(err)
	}
	return &c, nil
}

func (r conversationRepo) Create(ctx context.Context, c *store.Conversation) error {
	if c.Thinking == "" {
		c.Thinking = store.ThinkingOff
	}
	return mapErr(r.db.WithContext(ctx).Create(c).Error)
}

func (r conversationRepo) Get(ctx context.Context, userID, id uint64) (*store.Conversation, error) {
	var c store.Conversation
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&c).Error; err != nil {
		return nil, mapErr(err)
	}
	return &c, nil
}

func (r conversationRepo) List(ctx context.Context, userID uint64) ([]store.Conversation, error) {
	var out []store.Conversation
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("updated_at DESC, id DESC").Find(&out).Error
	return out, mapErr(err)
}

func (r conversationRepo) Update(ctx context.Context, userID, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&store.Conversation{}).Where("id = ? AND user_id = ?", id, userID).Updates(fields)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r conversationRepo) Delete(ctx context.Context, userID, id uint64) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&store.Conversation{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return store.ErrNotFound
		}
		return tx.Where("conversation_id = ? AND user_id = ?", id, userID).Delete(&store.Message{}).Error
	}))
}

func (r conversationRepo) Touch(ctx context.Context, userID, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&store.Conversation{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("last_message_at", at)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r conversationRepo) ClearModel(ctx context.Context, modelID uint64) error {
	return mapErr(r.db.WithContext(ctx).Model(&store.Conversation{}).
		Where("model_id = ?", modelID).UpdateColumn("model_id", nil).Error)
}

// ---------- models ----------

type modelRepo struct{ db *gorm.DB }

func (r modelRepo) Create(ctx context.Context, m *store.ModelConfig) error {
	return mapErr(r.db.WithContext(ctx).Create(m).Error)
}

func (r modelRepo) Get(ctx context.Context, id uint64) (*store.ModelConfig, error) {
	var m store.ModelConfig
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, mapErr(err)
	}
	return &m, nil
}

func (r modelRepo) ListVisible(ctx context.Context, userID uint64) ([]store.ModelConfig, error) {
	var out []store.ModelConfig
	err := r.db.WithContext(ctx).Where("owner_id IN ?", []uint64{store.PublicOwner, userID}).
		Order("owner_id, id").Find(&out).Error
	return out, mapErr(err)
}

func (r modelRepo) Update(ctx context.Context, m *store.ModelConfig) error {
	res := r.db.WithContext(ctx).Model(&store.ModelConfig{}).Where("id = ?", m.ID).Updates(map[string]any{
		"name": m.Name, "base_url": m.BaseURL, "api_key_enc": m.APIKey, "model": m.Model,
		"thinking_style": m.ThinkingStyle, "thinking_model": m.ThinkingModel,
		"temperature": m.Temperature, "max_tokens": m.MaxTokens,
	})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r modelRepo) Delete(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Delete(&store.ModelConfig{}, id)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r modelRepo) ClearDefault(ctx context.Context, ownerID uint64) error {
	return mapErr(r.db.WithContext(ctx).Model(&store.ModelConfig{}).Where("owner_id = ?", ownerID).Update("is_default", false).Error)
}

func (r modelRepo) SetDefault(ctx context.Context, ownerID, id uint64) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&store.ModelConfig{}).Where("owner_id = ?", ownerID).Update("is_default", false).Error; err != nil {
			return err
		}
		res := tx.Model(&store.ModelConfig{}).Where("id = ? AND owner_id = ?", id, ownerID).Update("is_default", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return store.ErrNotFound
		}
		return nil
	}))
}

// ---------- messages ----------

type messageRepo struct{ db *gorm.DB }

func (r messageRepo) Create(ctx context.Context, m *store.Message) error {
	if m.Status == "" {
		m.Status = store.MessageComplete
	}
	return mapErr(r.db.WithContext(ctx).Create(m).Error)
}

func (r messageRepo) Finish(ctx context.Context, m *store.Message) error {
	res := r.db.WithContext(ctx).Model(&store.Message{}).
		Where("id = ? AND user_id = ?", m.ID, m.UserID).
		Updates(map[string]any{
			"content":           m.Content,
			"status":            m.Status,
			"model":             m.Model,
			"prompt_tokens":     m.PromptTokens,
			"completion_tokens": m.CompletionTokens,
			"reasoning":         m.Reasoning,
			"tool_log":          m.ToolLog,
		})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r messageRepo) Recent(ctx context.Context, userID, conversationID uint64, limit int) ([]store.Message, error) {
	return r.Before(ctx, userID, conversationID, 0, limit)
}

func (r messageRepo) After(ctx context.Context, userID, conversationID, afterID uint64, limit int) ([]store.Message, error) {
	var out []store.Message
	err := r.db.WithContext(ctx).Where("user_id = ? AND conversation_id = ? AND id > ?", userID, conversationID, afterID).
		Order("id").Limit(limit).Find(&out).Error
	return out, mapErr(err)
}

func (r messageRepo) LastUserMessage(ctx context.Context, userID uint64) (*store.Message, error) {
	var m store.Message
	err := r.db.WithContext(ctx).Where("user_id = ? AND role = ?", userID, store.RoleUser).Order("id DESC").Limit(1).Take(&m).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &m, nil
}

func (r messageRepo) LatestID(ctx context.Context, userID, conversationID uint64) (uint64, error) {
	var id uint64
	err := r.db.WithContext(ctx).Model(&store.Message{}).Where("user_id = ? AND conversation_id = ?", userID, conversationID).
		Select("COALESCE(MAX(id), 0)").Scan(&id).Error
	return id, mapErr(err)
}

func (r messageRepo) Before(ctx context.Context, userID, conversationID, beforeID uint64, limit int) ([]store.Message, error) {
	q := r.db.WithContext(ctx).Where("user_id = ? AND conversation_id = ?", userID, conversationID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var out []store.Message
	if err := q.Order("id DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, mapErr(err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ---------- memories ----------

type memoryRepo struct{ db *gorm.DB }

func (r memoryRepo) Create(ctx context.Context, m *store.Memory) error {
	return mapErr(r.db.WithContext(ctx).Create(m).Error)
}

func (r memoryRepo) Get(ctx context.Context, userID, id uint64) (*store.Memory, error) {
	var m store.Memory
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&m).Error; err != nil {
		return nil, mapErr(err)
	}
	return &m, nil
}

func (r memoryRepo) GetMany(ctx context.Context, userID uint64, ids []uint64) ([]store.Memory, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []store.Memory
	err := r.db.WithContext(ctx).Where("user_id = ? AND id IN ?", userID, ids).Find(&out).Error
	return out, mapErr(err)
}

func (r memoryRepo) List(ctx context.Context, userID uint64, f store.MemoryFilter, limit, offset int) ([]store.Memory, int64, error) {
	q := r.db.WithContext(ctx).Model(&store.Memory{}).Where("user_id = ?", userID)
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.Query != "" {
		q = q.Where("content LIKE ?", "%"+escapeLike(f.Query)+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var out []store.Memory
	// 列表不需要向量，省去传输开销。
	err := q.Omit("embedding").Order("id DESC").Limit(limit).Offset(offset).Find(&out).Error
	return out, total, mapErr(err)
}

// escapeLike 去掉 LIKE 通配符与反斜杠。MySQL 与 SQLite 对反斜杠转义的默认行为不一致，
// 关键字搜索也不需要支持通配符，直接去掉最稳妥。
func escapeLike(s string) string {
	return strings.NewReplacer("%", "", "_", "", `\`, "").Replace(s)
}

func (r memoryRepo) LoadEmbeddings(ctx context.Context, userID uint64, model string) ([]store.EmbeddingRow, error) {
	var out []store.EmbeddingRow
	err := r.db.WithContext(ctx).Model(&store.Memory{}).
		Select("id", "embedding").
		Where("user_id = ? AND embedding_model = ? AND embedding IS NOT NULL", userID, model).
		Order("id").
		Scan(&out).Error
	return out, mapErr(err)
}

func (r memoryRepo) TouchAccessed(ctx context.Context, userID uint64, ids []uint64, boost float64, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	err := r.db.WithContext(ctx).Model(&store.Memory{}).
		Where("user_id = ? AND id IN ?", userID, ids).
		Updates(map[string]any{
			"last_accessed_at": at,
			"access_count":     gorm.Expr("access_count + 1"),
			"strength":         gorm.Expr("strength + ?", boost),
		}).Error
	return mapErr(err)
}

func (r memoryRepo) Delete(ctx context.Context, userID, id uint64) error {
	// 带 DeletedAt 的模型会自动追加 deleted_at IS NULL，已删除的记忆返回 ErrNotFound。
	res := r.db.WithContext(ctx).Model(&store.Memory{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{
			"content":         "",
			"embedding":       nil,
			"embedding_model": "",
			"embedding_dim":   0,
			"metadata":        "",
			"deleted_at":      time.Now().UTC(),
		})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r memoryRepo) RecordUsage(ctx context.Context, usages []store.MemoryUsage) error {
	if len(usages) == 0 {
		return nil
	}
	return mapErr(r.db.WithContext(ctx).Create(&usages).Error)
}

func (r memoryRepo) ListForReembed(ctx context.Context, afterID uint64, model string, onlyStale bool, limit int) ([]store.Memory, error) {
	q := r.db.WithContext(ctx).Where("id > ?", afterID)
	if onlyStale {
		q = q.Where("(embedding_model <> ? OR embedding IS NULL)", model)
	}
	var out []store.Memory
	err := q.Omit("embedding").Order("id").Limit(limit).Find(&out).Error
	return out, mapErr(err)
}

func (r memoryRepo) UpdateEmbedding(ctx context.Context, id uint64, embedding []byte, model string, dim int) error {
	res := r.db.WithContext(ctx).Model(&store.Memory{}).Where("id = ?", id).
		Updates(map[string]any{"embedding": embedding, "embedding_model": model, "embedding_dim": dim})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r memoryRepo) CountByModel(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		EmbeddingModel string
		N              int64
	}
	err := r.db.WithContext(ctx).Model(&store.Memory{}).
		Select("embedding_model, COUNT(*) AS n").Group("embedding_model").Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.EmbeddingModel] = row.N
	}
	return out, nil
}

// ---------- usage ----------

type usageRepo struct{ db *gorm.DB }

func (r usageRepo) Get(ctx context.Context, userID uint64, day string) (store.UsageDaily, error) {
	var u store.UsageDaily
	err := r.db.WithContext(ctx).Where("user_id = ? AND day = ?", userID, day).First(&u).Error
	if err != nil {
		if mapErr(err) == store.ErrNotFound {
			return store.UsageDaily{UserID: userID, Day: day}, nil
		}
		return store.UsageDaily{}, mapErr(err)
	}
	return u, nil
}

func (r usageRepo) Add(ctx context.Context, userID uint64, day string, messages, promptTokens, completionTokens int) error {
	row := store.UsageDaily{
		UserID: userID, Day: day,
		Messages: messages, PromptTokens: promptTokens, CompletionTokens: completionTokens,
	}
	// MySQL 生成 ON DUPLICATE KEY UPDATE，SQLite 生成 ON CONFLICT DO UPDATE；
	// 两者中未限定的列名都指向已存在的行。
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"messages":          gorm.Expr("messages + ?", messages),
			"prompt_tokens":     gorm.Expr("prompt_tokens + ?", promptTokens),
			"completion_tokens": gorm.Expr("completion_tokens + ?", completionTokens),
			"updated_at":        time.Now().UTC(),
		}),
	}).Create(&row).Error
	return mapErr(err)
}

// ---------- jobs ----------

type jobRepo struct{ db *gorm.DB }

func (r jobRepo) Start(ctx context.Context, name string, at time.Time) (*store.JobRun, error) {
	run := &store.JobRun{JobName: name, StartedAt: at, Status: store.JobRunning}
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, mapErr(err)
	}
	return run, nil
}

func (r jobRepo) Finish(ctx context.Context, id uint64, status, errMsg string, at time.Time) error {
	if len(errMsg) > 4000 {
		errMsg = errMsg[:4000]
	}
	res := r.db.WithContext(ctx).Model(&store.JobRun{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "error": errMsg, "finished_at": at})
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r jobRepo) LastSucceeded(ctx context.Context, name string) (*store.JobRun, error) {
	var run store.JobRun
	err := r.db.WithContext(ctx).Where("job_name = ? AND status = ?", name, store.JobSucceeded).
		Order("started_at DESC").First(&run).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return &run, nil
}

// ---------- meta ----------

type metaRepo struct{ db *gorm.DB }

func (r metaRepo) Get(ctx context.Context, key string) (string, bool, error) {
	var m store.AppMeta
	err := r.db.WithContext(ctx).Where("meta_key = ?", key).First(&m).Error
	if err != nil {
		if mapErr(err) == store.ErrNotFound {
			return "", false, nil
		}
		return "", false, mapErr(err)
	}
	return m.Value, true, nil
}

func (r metaRepo) Set(ctx context.Context, key, value string) error {
	m := store.AppMeta{Key: key, Value: value, UpdatedAt: time.Now().UTC()}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "meta_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&m).Error
	return mapErr(err)
}
