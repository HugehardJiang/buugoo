package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "buugoo"

// Claims 是 JWT 中携带的信息。
type Claims struct {
	// TokenVersion 必须与用户当前的 token_version 一致，否则令牌已失效。
	TokenVersion int `json:"tv"`
	jwt.RegisteredClaims
}

// UserID 从 Subject 中解析用户 ID。
func (c *Claims) UserID() (uint64, error) {
	return strconv.ParseUint(c.Subject, 10, 64)
}

// Tokens 负责签发与解析 JWT（HS256）。
type Tokens struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokens 创建令牌工具。
func NewTokens(secret string, ttl time.Duration) (*Tokens, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT 密钥至少 32 个字符")
	}
	return &Tokens{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

// Issue 为用户签发令牌。
func (t *Tokens) Issue(userID uint64, tokenVersion int) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	claims := Claims{
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.FormatUint(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("签发令牌: %w", err)
	}
	return s, exp, nil
}

// Parse 校验并解析令牌，任何校验失败都返回 ErrUnauthorized。
func (t *Tokens) Parse(token string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	return &c, nil
}
