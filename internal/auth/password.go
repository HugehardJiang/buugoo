package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// HashPassword 生成密码哈希。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希: %w", err)
	}
	return string(b), nil
}

// CheckPassword 校验密码，不匹配时返回 ErrInvalidCredentials。
func CheckPassword(hash, pw string) error {
	if hash == "" {
		return ErrInvalidCredentials
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("校验密码: %w", err)
	}
	return nil
}
