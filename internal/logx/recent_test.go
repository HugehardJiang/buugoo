package logx

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecent(t *testing.T) {
	var out bytes.Buffer
	log, err := New(&out, "info", "text")
	if err != nil {
		t.Fatal(err)
	}
	Recent = &Ring{buf: make([]Entry, 3)}
	log.Info("普通日志不留")
	l := log.With("component", "chat")
	l.Warn("对话生成失败", "user_id", 7, "err", "HTTP 401 Authorization: Bearer sk-abcdefghijklmnop invalid", "api_key", "sk-zzzzzzzzzzzz")
	for i := 0; i < 4; i++ {
		log.Error("出错", "i", i)
	}
	all := Recent.List(10, nil)
	if len(all) != 3 || all[2].Attrs["i"] != "3" {
		t.Fatalf("ring = %+v", all)
	}
	Recent = &Ring{buf: make([]Entry, 10)}
	l.Warn("对话生成失败", "user_id", 7, "err", "HTTP 401 Authorization: Bearer sk-abcdefghijklmnop invalid", "api_key", "sk-zzzzzzzzzzzz")
	e := Recent.List(10, func(e Entry) bool { return e.Attrs["user_id"] == "7" })
	if len(e) != 1 || e[0].Attrs["component"] != "chat" {
		t.Fatalf("%+v", e)
	}
	got := e[0].Attrs["err"] + e[0].Attrs["api_key"]
	if strings.Contains(got, "abcdefghijklmnop") || strings.Contains(got, "zzzzzzzz") {
		t.Fatalf("密钥没有打码: %q", got)
	}
	if !strings.Contains(out.String(), "普通日志不留") {
		t.Error("原来的输出不受影响")
	}
}
