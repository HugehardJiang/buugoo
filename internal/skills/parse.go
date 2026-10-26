// Package skills 支持安装标准格式的 skill（Anthropic Agent Skills：一个文件夹，里面有 SKILL.md，
// 开头的 YAML 写名字和说明，正文是给模型看的使用说明，另外可以带脚本和参考文件）。
//
// 管理员安装给全站，每个用户可以自己关掉。聊天时模型先只看到每个 skill 的名字和说明，
// 需要时再读完整的说明和文件；skill 的脚本在运行代码的沙箱里执行，只有允许联网的 skill 能联网。
package skills

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// 限制。
const (
	maxNameLen        = 64
	maxDescriptionLen = 1024
	// MaxSkillBytes 是一个 skill 的文件总大小上限，maxSkillFiles 是文件数上限。
	MaxSkillBytes = 20 << 20
	maxSkillFiles = 1000
)

// Meta 是 SKILL.md 开头的元数据。
type Meta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// normalizeName 把名字规范成小写字母、数字和连字符（标准要求的格式）；中文等其他字符去掉。
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// Parse 解析 SKILL.md：返回元数据和正文。fallback 是元数据里没有名字时使用的名字（一般是文件夹名）。
func Parse(data []byte, fallback string) (Meta, string, error) {
	text := string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !utf8.ValidString(text) {
		return Meta{}, "", errors.New("SKILL.md 不是 UTF-8 文本")
	}
	var m Meta
	body := text
	if strings.HasPrefix(text, "---\n") {
		end := strings.Index(text[4:], "\n---")
		if end < 0 {
			return Meta{}, "", errors.New("SKILL.md 开头的 --- 没有结束")
		}
		if err := yaml.Unmarshal([]byte(text[4:4+end]), &m); err != nil {
			return Meta{}, "", fmt.Errorf("SKILL.md 开头的元数据格式不对：%v", err)
		}
		body = strings.TrimLeft(text[4+end+4:], "-\n")
	}
	if m.Name == "" {
		m.Name = fallback
	}
	m.Name = normalizeName(m.Name)
	m.Description = strings.Join(strings.Fields(m.Description), " ")
	switch {
	case m.Name == "" || !nameRe.MatchString(m.Name):
		return Meta{}, "", errors.New("SKILL.md 里没有有效的 name（只能用小写字母、数字和连字符）")
	case len(m.Name) > maxNameLen:
		return Meta{}, "", fmt.Errorf("name 太长（最多 %d 个字符）", maxNameLen)
	case m.Description == "":
		return Meta{}, "", errors.New("SKILL.md 里没有 description（说明这个 skill 做什么、什么时候用）")
	}
	if utf8.RuneCountInString(m.Description) > maxDescriptionLen {
		m.Description = string([]rune(m.Description)[:maxDescriptionLen])
	}
	return m, strings.TrimSpace(body), nil
}
