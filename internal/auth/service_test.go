package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func newStore(t *testing.T) store.Store {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "a.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func newCfg(mode, registration string) *config.Config {
	c := config.Default()
	c.Mode = mode
	c.Auth.Registration = registration
	c.Auth.JWTSecret = "0123456789abcdef0123456789abcdef"
	return c
}

func TestSingleModeNoPassword(t *testing.T) {
	ctx := context.Background()
	cfg := newCfg(config.ModeSingle, config.RegistrationClosed)
	cfg.Auth.JWTSecret = "" // single 模式应自动生成并持久化
	s := newStore(t)
	svc, err := NewService(ctx, cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	if svc.PasswordRequired() {
		t.Fatal("未设置密码时不应要求登录")
	}
	u, err := svc.Authenticate(ctx, "")
	if err != nil || u.ID != store.OwnerID {
		t.Fatalf("u=%v err=%v", u, err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Username: "bob", Password: "12345678"}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("single 模式不应支持注册: %v", err)
	}
	// 重启后（新建 Service）密钥不变。
	svc2, err := NewService(ctx, cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	if string(svc.tokens.secret) != string(svc2.tokens.secret) {
		t.Fatal("持久化的 JWT 密钥应保持不变")
	}
}

func TestSingleModeWithPassword(t *testing.T) {
	ctx := context.Background()
	cfg := newCfg(config.ModeSingle, config.RegistrationClosed)
	cfg.Auth.AccessPassword = "secret-pass"
	svc, err := NewService(ctx, cfg, newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("应要求登录: %v", err)
	}
	if _, err := svc.Login(ctx, "", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("错误密码应失败: %v", err)
	}
	sess, err := svc.Login(ctx, "", "secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.Authenticate(ctx, sess.Token)
	if err != nil || u.ID != store.OwnerID {
		t.Fatalf("u=%v err=%v", u, err)
	}
	if err := svc.LogoutAll(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("退出所有设备后旧令牌应失效: %v", err)
	}
}

func TestMultiModeInviteRegistration(t *testing.T) {
	ctx := context.Background()
	svc, err := NewService(ctx, newCfg(config.ModeMulti, config.RegistrationInvite), newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Username: "alice", Password: "password1"}); !errors.Is(err, ErrInviteRequired) {
		t.Fatalf("应要求邀请码: %v", err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Username: "alice", Password: "password1", InviteCode: "NOPE"}); !errors.Is(err, store.ErrInviteInvalid) {
		t.Fatalf("无效邀请码: %v", err)
	}
	inv, err := svc.CreateInvite(ctx, 0, InviteInput{MaxUses: 1, TTL: time.Hour})
	if err != nil || len(inv.Code) != 10 {
		t.Fatalf("inv=%v err=%v", inv, err)
	}
	var verr *ValidationError
	if _, err := svc.Register(ctx, RegisterInput{Username: "a", Password: "password1", InviteCode: inv.Code}); !errors.As(err, &verr) {
		t.Fatalf("用户名过短应校验失败: %v", err)
	}
	sess, err := svc.Register(ctx, RegisterInput{Username: "alice", Password: "password1", InviteCode: inv.Code})
	if err != nil {
		t.Fatal(err)
	}
	if sess.User.InviteID == nil || *sess.User.InviteID != inv.ID {
		t.Fatal("应记录所用邀请码")
	}
	if _, err := svc.Register(ctx, RegisterInput{Username: "bob", Password: "password1", InviteCode: inv.Code}); !errors.Is(err, store.ErrInviteInvalid) {
		t.Fatalf("邀请码已用完: %v", err)
	}
	if _, err := svc.Login(ctx, "alice", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "nobody", "password1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	s2, err := svc.Login(ctx, "alice", "password1")
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.Authenticate(ctx, s2.Token)
	if err != nil || u.Username != "alice" {
		t.Fatalf("u=%v err=%v", u, err)
	}
	s3, err := svc.ChangePassword(ctx, u, "password1", "password2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, s2.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("改密码后旧令牌应失效")
	}
	if _, err := svc.Authenticate(ctx, s3.Token); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationModes(t *testing.T) {
	ctx := context.Background()
	closed, err := NewService(ctx, newCfg(config.ModeMulti, config.RegistrationClosed), newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.Register(ctx, RegisterInput{Username: "alice", Password: "password1"}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatal(err)
	}
	open, err := NewService(ctx, newCfg(config.ModeMulti, config.RegistrationOpen), newStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open.Register(ctx, RegisterInput{Username: "alice", Password: "password1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := open.Register(ctx, RegisterInput{Username: "alice", Password: "password1"}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatal(err)
	}
}

func TestTokens(t *testing.T) {
	tk, err := NewTokens("0123456789abcdef0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := tk.Issue(42, 3)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tk.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := c.UserID(); id != 42 || c.TokenVersion != 3 {
		t.Fatalf("claims=%+v", c)
	}
	other, _ := NewTokens("ffffffffffffffffffffffffffffffff", time.Hour)
	if _, err := other.Parse(s); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("不同密钥签发的令牌应无效")
	}
	tk.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := tk.Parse(s); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("过期令牌应无效")
	}
	if _, err := NewTokens("short", time.Hour); err == nil {
		t.Fatal("过短密钥应报错")
	}
}
