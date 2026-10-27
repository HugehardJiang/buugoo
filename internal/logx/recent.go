package logx

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 最近的警告和错误留在内存里（不落盘），给网页上的「复制诊断信息」用。只留 WARN 及以上，
// 值里像密钥的部分会被打码。

// recentSize 是留下的条数。
const recentSize = 400

// Entry 是一条日志。
type Entry struct {
	Time  time.Time         `json:"time"`
	Level string            `json:"level"`
	Msg   string            `json:"msg"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Ring 是最近日志的环形缓冲。
type Ring struct {
	mu   sync.Mutex
	buf  []Entry
	next int
	full bool
}

// Recent 是全局的最近日志（logx.New 创建的 logger 都会写进来）。
var Recent = &Ring{buf: make([]Entry, recentSize)}

func (r *Ring) add(e Entry) {
	r.mu.Lock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// List 按时间顺序返回符合条件的最近 n 条（match 为空表示全部）。
func (r *Ring) List(n int, match func(Entry) bool) []Entry {
	r.mu.Lock()
	var all []Entry
	if r.full {
		all = append(all, r.buf[r.next:]...)
	}
	all = append(all, r.buf[:r.next]...)
	r.mu.Unlock()
	out := []Entry{}
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		if match == nil || match(all[i]) {
			out = append(out, all[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|password|secret|authorization)["']?\s*[:=]\s*["']?)[^\s"'&,}]{4,}`),
}

// Redact 把文字里像密钥、令牌、密码的部分打码。
func Redact(s string) string {
	for i, re := range secretPatterns {
		if i == 0 {
			s = re.ReplaceAllStringFunc(s, func(m string) string { return m[:5] + "****" })
			continue
		}
		s = re.ReplaceAllString(s, "${1}****")
	}
	return s
}

// secretKey 报告属性名是不是本身就是密钥（这类值整个不留）。
func secretKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range []string{"key", "token", "password", "secret", "authorization", "dsn", "cookie"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// recentHandler 把 WARN 及以上的日志同时记进 Recent。
type recentHandler struct {
	inner slog.Handler
	attrs []slog.Attr
	group string
}

func (h *recentHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *recentHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		e := Entry{Time: r.Time, Level: r.Level.String(), Msg: Redact(r.Message), Attrs: map[string]string{}}
		put := func(a slog.Attr) {
			k := a.Key
			if h.group != "" {
				k = h.group + "." + k
			}
			v := a.Value.Resolve().String()
			if secretKey(a.Key) {
				v = "****"
			}
			if len(v) > 2000 {
				v = v[:2000] + "…"
			}
			e.Attrs[k] = Redact(v)
		}
		for _, a := range h.attrs {
			put(a)
		}
		r.Attrs(func(a slog.Attr) bool { put(a); return true })
		Recent.add(e)
	}
	return h.inner.Handle(ctx, r)
}

func (h *recentHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recentHandler{inner: h.inner.WithAttrs(attrs), attrs: append(append([]slog.Attr{}, h.attrs...), attrs...), group: h.group}
}

func (h *recentHandler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &recentHandler{inner: h.inner.WithGroup(name), attrs: h.attrs, group: g}
}
