package httpapi

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"github.com/HugehardJiang/buugoo/internal/accounts"
	"github.com/HugehardJiang/buugoo/internal/billing"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 会员、卡密与用户管理。管理接口都在管理后台（管理员密码）下面，并且管理员密码还是默认的时候不能用。

func (s *server) routeBilling(api, priv, console *echo.Group) {
	if s.Billing == nil || s.Accounts == nil {
		return
	}
	m := console.Group("", s.requireStrongConsole)
	m.GET("/users", s.consoleUsers)
	m.GET("/users/:id", s.consoleUser)
	m.POST("/users/:id/disable", s.consoleDisableUser)
	m.POST("/users/:id/admin", s.consoleSetAdmin)
	m.POST("/users/:id/password", s.consoleResetPassword)
	m.POST("/users/:id/grant", s.consoleGrant)
	m.POST("/users/:id/adjust", s.consoleAdjust)
	m.POST("/users/:id/delete", s.consoleDeleteUser)
	m.GET("/plans", s.consolePlans)
	m.POST("/plans", s.consoleCreatePlan)
	m.PUT("/plans/:id", s.consoleUpdatePlan)
	m.POST("/plans/:id/visible", s.consolePlanVisible)
	m.DELETE("/plans/:id", s.consoleDeletePlan)
	m.GET("/pricing", s.consolePricing)
	m.PUT("/pricing", s.consoleSetPricing)
	m.GET("/cards", s.consoleCards)
	m.POST("/cards/generate", s.consoleGenerateCards)
	m.POST("/cards/disable", s.consoleDisableCards(true))
	m.POST("/cards/enable", s.consoleDisableCards(false))
	m.POST("/cards/delete", s.consoleDeleteCards)
	m.GET("/cards/export", s.consoleExportCards)

	priv.GET("/billing", s.myBilling)
	// 兑换卡密：每个 IP 每分钟最多 10 次，另外每个账号连续输错 8 次锁 30 分钟。
	redeemLimiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
			Rate: rate.Every(6 * time.Second), Burst: 10, ExpiresIn: 30 * time.Minute,
		}),
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			return &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "尝试次数过多，请稍后再试"}
		},
	})
	priv.POST("/billing/redeem", s.redeem, redeemLimiter)
}

// requireStrongConsole 在管理员密码还是默认的 1228 时拒绝会员和用户管理（谁都知道这个密码）。
func (s *server) requireStrongConsole(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.defaultAdminPassword() {
			return &apiError{Status: http.StatusForbidden, Code: "weak_admin_password",
				Message: "管理员密码还是默认的 1228，谁都能登录。先在配置文件里改掉 admin.password 并重启，才能管理用户和会员"}
		}
		return next(c)
	}
}

func (s *server) audit(c echo.Context, msg string, args ...any) {
	s.log.Warn(msg, append(args, "ip", c.RealIP())...)
}

// ---------- 用户 ----------

func (s *server) consoleUsers(c echo.Context) error {
	limit, err := queryInt(c, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	offset, err := queryInt(c, "offset", 0, 0, 1<<30)
	if err != nil {
		return err
	}
	rows, total, err := s.Accounts.List(c.Request().Context(), accounts.Filter{Query: c.QueryParam("q"), Status: c.QueryParam("status")}, limit, offset)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"users": rows, "total": total})
}

func (s *server) consoleUser(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	u, err := s.Accounts.Get(ctx, id)
	if err != nil {
		return err
	}
	acc, err := s.Billing.Account(ctx, id, 50)
	if err != nil {
		return err
	}
	st, err := s.Quota.Status(ctx, u)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"user": toUser(u), "disabled_at": u.DisabledAt, "disabled_reason": u.DisabledReason,
		"account": acc, "quota": st, "labels": s.costLabels(c)})
}

func (s *server) consoleDisableUser(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Disabled bool   `json:"disabled"`
		Reason   string `json:"reason"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.Accounts.SetDisabled(c.Request().Context(), id, req.Disabled, req.Reason); err != nil {
		return err
	}
	s.audit(c, "管理后台停用/恢复了用户", "user", id, "disabled", req.Disabled)
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleSetAdmin(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Admin bool `json:"admin"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.Accounts.SetAdmin(c.Request().Context(), id, req.Admin); err != nil {
		return err
	}
	s.audit(c, "管理后台修改了管理员", "user", id, "admin", req.Admin)
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleResetPassword(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.Auth.ResetPassword(c.Request().Context(), id, req.Password); err != nil {
		return err
	}
	s.audit(c, "管理后台重置了用户密码", "user", id)
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleGrant(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		PlanID uint64 `json:"plan_id"`
		Note   string `json:"note"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := s.Accounts.Get(ctx, id); err != nil {
		return err
	}
	msg, err := s.Billing.Grant(ctx, id, req.PlanID, req.Note)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台给用户开通了套餐", "user", id, "plan", req.PlanID)
	return c.JSON(http.StatusOK, map[string]any{"message": msg})
}

func (s *server) consoleAdjust(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Amount int64  `json:"amount"` // 微元，正数加、负数扣
		Note   string `json:"note"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := s.Accounts.Get(ctx, id); err != nil {
		return err
	}
	if err := s.Billing.Adjust(ctx, id, req.Amount, req.Note); err != nil {
		return err
	}
	s.audit(c, "管理后台调整了用户余额", "user", id, "amount_yuan", quota.Yuan(req.Amount))
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleDeleteUser(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.Accounts.Delete(c.Request().Context(), id, req.Confirm); err != nil {
		return err
	}
	s.audit(c, "管理后台删除了用户", "user", id, "username", req.Confirm)
	return c.NoContent(http.StatusNoContent)
}

// ---------- 套餐 ----------

func (s *server) consolePlans(c echo.Context) error {
	ps, err := s.Billing.Plans(c.Request().Context(), false)
	if err != nil {
		return err
	}
	out := make([]planDTO, len(ps))
	for i, p := range ps {
		out[i] = toPlanDTO(p, true)
	}
	return c.JSON(http.StatusOK, map[string]any{"plans": out})
}

func (s *server) consoleCreatePlan(c echo.Context) error {
	var in billing.PlanInput
	if err := bind(c, &in); err != nil {
		return err
	}
	p, err := s.Billing.CreatePlan(c.Request().Context(), in)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台新建了套餐", "plan", p.ID, "name", p.Name)
	return c.JSON(http.StatusOK, toPlanDTO(*p, true))
}

func (s *server) consoleUpdatePlan(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var in billing.PlanInput
	if err := bind(c, &in); err != nil {
		return err
	}
	p, err := s.Billing.UpdatePlan(c.Request().Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台修改了套餐", "plan", id)
	return c.JSON(http.StatusOK, toPlanDTO(*p, true))
}

func (s *server) consolePlanVisible(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Visible bool `json:"visible"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.Billing.SetVisible(c.Request().Context(), id, req.Visible); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleDeletePlan(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Billing.DeletePlan(c.Request().Context(), id); err != nil {
		return err
	}
	s.audit(c, "管理后台删除了套餐", "plan", id)
	return c.NoContent(http.StatusNoContent)
}

// ---------- 卡密 ----------

func cardFilter(c echo.Context) billing.CardFilter {
	plan, _ := strconv.ParseUint(c.QueryParam("plan"), 10, 64)
	return billing.CardFilter{PlanID: plan, Status: c.QueryParam("status"), Batch: c.QueryParam("batch"), Query: c.QueryParam("q")}
}

func (s *server) consoleCards(c echo.Context) error {
	limit, err := queryInt(c, "limit", 50, 1, 500)
	if err != nil {
		return err
	}
	offset, err := queryInt(c, "offset", 0, 0, 1<<30)
	if err != nil {
		return err
	}
	list, total, err := s.Billing.Cards(c.Request().Context(), cardFilter(c), limit, offset)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"cards": list, "total": total})
}

func (s *server) consoleGenerateCards(c echo.Context) error {
	var in billing.GenerateInput
	if err := bind(c, &in); err != nil {
		return err
	}
	g, err := s.Billing.Generate(c.Request().Context(), in)
	if err != nil {
		return err
	}
	// 只记数量和批次，不记卡密原文。
	s.audit(c, "管理后台生成了卡密", "plan", in.PlanID, "count", len(g.Codes), "batch", g.Batch)
	return c.JSON(http.StatusOK, g)
}

func idsBody(c echo.Context) ([]uint64, error) {
	var req struct {
		IDs []uint64 `json:"ids"`
	}
	if err := bind(c, &req); err != nil {
		return nil, err
	}
	if len(req.IDs) > 5000 {
		return nil, badRequest("一次最多 5000 张")
	}
	return req.IDs, nil
}

func (s *server) consoleDisableCards(disabled bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		ids, err := idsBody(c)
		if err != nil {
			return err
		}
		n, err := s.Billing.SetCardsDisabled(c.Request().Context(), ids, disabled)
		if err != nil {
			return err
		}
		s.audit(c, "管理后台停用/恢复了卡密", "count", n, "disabled", disabled)
		return c.JSON(http.StatusOK, map[string]any{"changed": n})
	}
}

func (s *server) consoleDeleteCards(c echo.Context) error {
	ids, err := idsBody(c)
	if err != nil {
		return err
	}
	n, err := s.Billing.DeleteCards(c.Request().Context(), ids)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台删除了卡密", "count", n)
	return c.JSON(http.StatusOK, map[string]any{"deleted": n})
}

// consoleExportCards 导出卡密列表（CSV）。只有打码后的卡密，原文在生成时已经下载过了。
func (s *server) consoleExportCards(c echo.Context) error {
	list, _, err := s.Billing.Cards(c.Request().Context(), cardFilter(c), 100000, 0)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\ufeff") // 让 Excel 认出 UTF-8
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"卡密（打码）", "套餐", "批次", "备注", "状态", "兑换截止", "兑换用户", "兑换时间", "生成时间"})
	label := map[string]string{store.CardUnused: "未使用", store.CardUsed: "已使用", store.CardDisabled: "已停用"}
	tf := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.In(s.Config.Location()).Format("2006-01-02 15:04")
	}
	for _, x := range list {
		created := x.CreatedAt
		_ = w.Write([]string{x.Hint, x.PlanName, x.Batch, x.Note, label[x.Status], tf(x.ExpiresAt), x.UsedName, tf(x.UsedAt), tf(&created)})
	}
	w.Flush()
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="cards-%s.csv"`, time.Now().Format("20060102-1504")))
	return c.Blob(http.StatusOK, "text/csv; charset=utf-8", []byte(b.String()))
}

// ---------- 用户自己 ----------

type planDTO struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Kind        string `json:"kind"`
	Days        int    `json:"days"`
	Cycle       string `json:"cycle"`
	// 时长套餐每周期的额度（0 表示不限）；充值套餐到账的金额（微元）。
	QuotaMessages int64  `json:"quota_messages"`
	QuotaTokens   int64  `json:"quota_tokens"`
	QuotaSearches int64  `json:"quota_searches"`
	Amount        int64  `json:"amount"`
	BuyURL        string `json:"buy_url"`
	// 下面两项只给管理后台。
	Visible *bool `json:"visible,omitempty"`
	Sort    *int  `json:"sort,omitempty"`
}

func toPlanDTO(p store.Plan, console bool) planDTO {
	d := planDTO{ID: p.ID, Name: p.Name, Description: p.Description, Price: p.Price, Kind: p.Kind,
		Days: p.Days, Cycle: p.Cycle, QuotaMessages: p.QuotaMessages, QuotaTokens: p.QuotaTokens, QuotaSearches: p.QuotaSearches,
		Amount: p.Amount, BuyURL: p.BuyURL}
	if console {
		d.Visible, d.Sort = &p.Visible, &p.Sort
	}
	return d
}

// myBilling 处理 GET /api/v1/billing：自己的会员情况、用量，以及可以买的套餐。
func (s *server) myBilling(c echo.Context) error {
	ctx, u := c.Request().Context(), currentUser(c)
	acc, err := s.Billing.Account(ctx, u.ID, 20)
	if err != nil {
		return err
	}
	st, err := s.Quota.Status(ctx, u)
	if err != nil {
		return err
	}
	ps, err := s.Billing.Plans(ctx, true)
	if err != nil {
		return err
	}
	plans := make([]planDTO, len(ps))
	for i, p := range ps {
		plans[i] = toPlanDTO(p, false)
	}
	return c.JSON(http.StatusOK, map[string]any{"account": acc, "quota": st, "plans": plans, "labels": s.costLabels(c)})
}

// redeemGuard 记每个账号连续兑换失败的次数，输错太多次锁一段时间（防止撞卡密）。
type redeemGuard struct {
	mu    sync.Mutex
	fails map[uint64]*redeemFail
}

type redeemFail struct {
	count int
	until time.Time
}

const (
	redeemMaxFails = 8
	redeemLock     = 30 * time.Minute
)

func (g *redeemGuard) locked(uid uint64) (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if f := g.fails[uid]; f != nil && time.Now().Before(f.until) {
		return f.until, true
	}
	return time.Time{}, false
}

func (g *redeemGuard) fail(uid uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fails == nil {
		g.fails = map[uint64]*redeemFail{}
	}
	f := g.fails[uid]
	if f == nil || (!f.until.IsZero() && time.Now().After(f.until)) {
		f = &redeemFail{}
		g.fails[uid] = f
	}
	f.count++
	if f.count >= redeemMaxFails {
		f.until, f.count = time.Now().Add(redeemLock), 0
	}
}

func (g *redeemGuard) ok(uid uint64) {
	g.mu.Lock()
	delete(g.fails, uid)
	g.mu.Unlock()
}

func (s *server) redeem(c echo.Context) error {
	var req struct {
		Code string `json:"code"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	u := currentUser(c)
	if until, locked := s.redeemGuard.locked(u.ID); locked {
		return &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited",
			Message: "输错的次数太多了，请在 " + until.In(s.Config.Location()).Format("15:04") + " 之后再试"}
	}
	msg, err := s.Billing.Redeem(c.Request().Context(), u.ID, req.Code)
	if errors.Is(err, billing.ErrCardInvalid) {
		s.redeemGuard.fail(u.ID)
		s.log.Warn("兑换卡密失败", "user", u.ID, "ip", c.RealIP())
		return err
	}
	if err != nil {
		return err
	}
	s.redeemGuard.ok(u.ID)
	s.log.Info("兑换了卡密", "user", u.ID, "ip", c.RealIP())
	return c.JSON(http.StatusOK, map[string]any{"message": msg})
}
