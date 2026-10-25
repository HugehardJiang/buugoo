// Package logx 基于标准库 slog 初始化日志。
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New 根据级别和格式创建 logger。format 为 text 或 json。
func New(w io.Writer, level, format string) (*slog.Logger, error) {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		return nil, fmt.Errorf("无效的日志级别 %q: %w", level, err)
	}
	opts := &slog.HandlerOptions{Level: lv}
	switch strings.ToLower(format) {
	case "", "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("无效的日志格式 %q（可选 text、json）", format)
	}
}
