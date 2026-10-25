// Package auth 实现 single / multi 两种模式的鉴权、注册与邀请码。
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 鉴权相关错误。
var (
	ErrUnauthorized       = errors.New("未登录或登录已失效")
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrForbidden          = errors.New("没有权限")
	ErrRegistrationClosed = errors.New("当前不开放注册")
	ErrInviteRequired     = errors.New("需要邀请码")
	ErrUsernameTaken      = errors.New("用户名已被使用")
	ErrNotSupported       = errors.New("当前模式不支持该操作")
)

// ValidationError 是用户输入校验错误。
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

const metaJWTSecret = "auth.jwt_secret"

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

// Session 是登录结果。
type Session struct {
	Token     string
	ExpiresAt time.Time
	User      *store.User
}

// Service 提供鉴权能力。
type Service struct {
	cfg    config.AuthConfig
	mode   string
	store  store.Store
	tokens *Tokens
	now    func() time.Time
}

// NewService 创建鉴权服务。single 模式下若未配置 JWT 密钥，会生成一个并持久化到数据库，
// 保证重启后令牌仍然有效。
func NewService(ctx context.Context, cfg *config.Config, s store.Store) (*Service, error) {
	secret := cfg.Auth.JWTSecret
	if secret == "" {
		if cfg.Mode == config.ModeMulti {
			return nil, errors.New("multi 模式必须配置 auth.jwt_secret")
		}
		var err error
		if secret, err = persistedSecret(ctx, s); err != nil {
			return nil, err
		}
	}
	tokens, err := NewTokens(secret, cfg.Auth.TokenTTL)
	if err != nil {
		return nil, err
	}
	svc := &Service{cfg: cfg.Auth, mode: cfg.Mode, store: s, tokens: tokens, now: time.Now}
	if cfg.Mode == config.ModeSingle {
		if _, err := s.Users().EnsureOwner(ctx); err != nil {
			return nil, fmt.Errorf("初始化单用户: %w", err)
		}
	}
	return svc, nil
}

func persistedSecret(ctx context.Context, s store.Store) (string, error) {
	v, ok, err := s.Meta().Get(ctx, metaJWTSecret)
	if err != nil {
		return "", fmt.Errorf("读取 JWT 密钥: %w", err)
	}
	if ok && len(v) >= 32 {
		return v, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 JWT 密钥: %w", err)
	}
	v = fmt.Sprintf("%x", b)
	if err := s.Meta().Set(ctx, metaJWTSecret, v); err != nil {
		return "", fmt.Errorf("保存 JWT 密钥: %w", err)
	}
	return v, nil
}

// Mode 返回部署模式。
func (s *Service) Mode() string { return s.mode }

// Registration 返回 multi 模式下的注册方式；single 模式返回 closed。
func (s *Service) Registration() string {
	if s.mode == config.ModeSingle {
		return config.RegistrationClosed
	}
	return s.cfg.Registration
}

// PasswordRequired 报告访问是否需要登录。single 模式未设置访问密码时无需登录。
func (s *Service) PasswordRequired() bool {
	return s.mode == config.ModeMulti || s.cfg.AccessPassword != ""
}

// Login 登录。single 模式只校验访问密码（username 被忽略）。
func (s *Service) Login(ctx context.Context, username, password string) (*Session, error) {
	if s.mode == config.ModeSingle {
		if s.cfg.AccessPassword != "" &&
			subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.AccessPassword)) != 1 {
			return nil, ErrInvalidCredentials
		}
		owner, err := s.store.Users().EnsureOwner(ctx)
		if err != nil {
			return nil, err
		}
		return s.issue(owner)
	}
	u, err := s.store.Users().GetByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		// 仍然做一次哈希比较，避免通过响应时间判断用户名是否存在。
		_ = CheckPassword(dummyHash(), password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := CheckPassword(u.PasswordHash, password); err != nil {
		return nil, err
	}
	return s.issue(u)
}

// dummyHash 是一个随机密码的哈希，仅用于平衡用户名不存在时的响应时间。
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword(rand.Text())
	if err != nil {
		return ""
	}
	return h
})

func (s *Service) issue(u *store.User) (*Session, error) {
	tok, exp, err := s.tokens.Issue(u.ID, u.TokenVersion)
	if err != nil {
		return nil, err
	}
	return &Session{Token: tok, ExpiresAt: exp, User: u}, nil
}

// RegisterInput 是注册参数。
type RegisterInput struct {
	Username    string
	Password    string
	DisplayName string
	InviteCode  string
}

// Register 注册新用户（仅 multi 模式）。
func (s *Service) Register(ctx context.Context, in RegisterInput) (*Session, error) {
	if s.mode != config.ModeMulti {
		return nil, ErrNotSupported
	}
	in.Username = strings.TrimSpace(in.Username)
	in.InviteCode = strings.ToUpper(strings.TrimSpace(in.InviteCode))
	if err := validateCredentials(in.Username, in.Password); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(in.DisplayName) > 32 {
		return nil, &ValidationError{"昵称最多 32 个字"}
	}
	switch s.cfg.Registration {
	case config.RegistrationClosed:
		return nil, ErrRegistrationClosed
	case config.RegistrationInvite:
		if in.InviteCode == "" {
			return nil, ErrInviteRequired
		}
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	u := &store.User{Username: in.Username, PasswordHash: hash, DisplayName: strings.TrimSpace(in.DisplayName)}
	err = s.store.Tx(ctx, func(tx store.Store) error {
		if s.cfg.Registration == config.RegistrationInvite {
			inv, err := tx.Invites().Redeem(ctx, in.InviteCode, s.now().UTC())
			if err != nil {
				return err
			}
			u.InviteID = &inv.ID
		}
		return tx.Users().Create(ctx, u)
	})
	if errors.Is(err, store.ErrConflict) {
		return nil, ErrUsernameTaken
	}
	if err != nil {
		return nil, err
	}
	return s.issue(u)
}

func validateCredentials(username, password string) error {
	if !usernameRe.MatchString(username) {
		return &ValidationError{"用户名需为 3-32 位字母、数字、下划线、点或短横线"}
	}
	if n := utf8.RuneCountInString(password); n < 8 || n > 128 {
		return &ValidationError{"密码长度需为 8-128 位"}
	}
	return nil
}

// Authenticate 校验令牌并返回对应用户。single 模式未设置访问密码时 token 可为空。
func (s *Service) Authenticate(ctx context.Context, token string) (*store.User, error) {
	if s.mode == config.ModeSingle && s.cfg.AccessPassword == "" {
		return s.store.Users().EnsureOwner(ctx)
	}
	if token == "" {
		return nil, ErrUnauthorized
	}
	c, err := s.tokens.Parse(token)
	if err != nil {
		return nil, err
	}
	id, err := c.UserID()
	if err != nil {
		return nil, ErrUnauthorized
	}
	u, err := s.store.Users().GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if u.TokenVersion != c.TokenVersion {
		return nil, ErrUnauthorized
	}
	return u, nil
}

// LogoutAll 使该用户的全部令牌失效。
func (s *Service) LogoutAll(ctx context.Context, userID uint64) error {
	return s.store.Users().BumpTokenVersion(ctx, userID)
}

// ChangePassword 修改密码（仅 multi 模式），成功后旧令牌全部失效并返回新会话。
func (s *Service) ChangePassword(ctx context.Context, u *store.User, oldPw, newPw string) (*Session, error) {
	if s.mode != config.ModeMulti {
		return nil, ErrNotSupported
	}
	if err := CheckPassword(u.PasswordHash, oldPw); err != nil {
		return nil, err
	}
	if err := validateCredentials(u.Username, newPw); err != nil {
		return nil, err
	}
	hash, err := HashPassword(newPw)
	if err != nil {
		return nil, err
	}
	if err := s.store.Users().UpdatePassword(ctx, u.ID, hash); err != nil {
		return nil, err
	}
	nu, err := s.store.Users().GetByID(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return s.issue(nu)
}

// CreateUser 由管理员或命令行直接创建用户（不受注册方式限制）。
func (s *Service) CreateUser(ctx context.Context, username, password string, admin bool) (*store.User, error) {
	if err := validateCredentials(username, password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &store.User{Username: username, PasswordHash: hash, IsAdmin: admin}
	if err := s.store.Users().Create(ctx, u); errors.Is(err, store.ErrConflict) {
		return nil, ErrUsernameTaken
	} else if err != nil {
		return nil, err
	}
	return u, nil
}

// InviteInput 是生成邀请码的参数。
type InviteInput struct {
	MaxUses int           // 0 表示不限次数
	TTL     time.Duration // 0 表示永不过期
	Note    string
}

// CreateInvite 生成邀请码。createdBy 为 0 表示由命令行生成。
func (s *Service) CreateInvite(ctx context.Context, createdBy uint64, in InviteInput) (*store.Invite, error) {
	if in.MaxUses < 0 || in.MaxUses > 100000 {
		return nil, &ValidationError{"可用次数需在 0-100000 之间（0 表示不限）"}
	}
	if in.TTL < 0 {
		return nil, &ValidationError{"有效期不能为负数"}
	}
	if utf8.RuneCountInString(in.Note) > 100 {
		return nil, &ValidationError{"备注最多 100 个字"}
	}
	code, err := newInviteCode()
	if err != nil {
		return nil, err
	}
	inv := &store.Invite{Code: code, CreatedBy: createdBy, MaxUses: in.MaxUses, Note: in.Note}
	if in.TTL > 0 {
		exp := s.now().UTC().Add(in.TTL)
		inv.ExpiresAt = &exp
	}
	if err := s.store.Invites().Create(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// 去掉了易混淆字符（0/O、1/I/L）。
const inviteAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

func newInviteCode() (string, error) {
	const n = 10
	out := make([]byte, 0, n)
	buf := make([]byte, 32)
	// 拒绝采样，避免取模带来的偏差。
	limit := byte(256 - 256%len(inviteAlphabet))
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成邀请码: %w", err)
		}
		for _, c := range buf {
			if c < limit && len(out) < n {
				out = append(out, inviteAlphabet[int(c)%len(inviteAlphabet)])
			}
		}
	}
	return string(out), nil
}
