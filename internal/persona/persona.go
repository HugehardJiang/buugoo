// Package persona 加载人格档案。对话中没有任何接口可以修改人格；只有管理员能在网页管理后台修改。
package persona

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/HugehardJiang/buugoo/configs"
)

// Persona 是人格档案。
type Persona struct {
	Name          string   `yaml:"name"`
	Identity      string   `yaml:"identity"`
	Personality   []string `yaml:"personality"`
	SpeakingStyle []string `yaml:"speaking_style"`
	Catchphrases  []string `yaml:"catchphrases"`
	Interests     []string `yaml:"interests"`
	Values        []string `yaml:"values"`
	// Attitude 是对待对方的态度（例如有主见、会反驳），可以为空。
	Attitude   []string `yaml:"attitude"`
	Principles []string `yaml:"principles"`
}

// Load 从文件加载人格档案；path 为空时使用内置的默认人格"布谷"。
func Load(path string) (Persona, error) {
	data := configs.DefaultPersona
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Persona{}, fmt.Errorf("读取人格档案: %w", err)
		}
		data = b
	}
	return Parse(data)
}

// ParseError 是人格档案的格式或内容错误。
type ParseError struct{ Err error }

func (e *ParseError) Error() string { return e.Err.Error() }
func (e *ParseError) Unwrap() error { return e.Err }

// Parse 解析并校验人格档案。未知字段视为错误，避免拼写错误被静默忽略。
func Parse(data []byte) (Persona, error) {
	var p Persona
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return Persona{}, &ParseError{fmt.Errorf("解析人格档案: %w", err)}
	}
	p = p.normalized()
	if err := p.Validate(); err != nil {
		return Persona{}, &ParseError{err}
	}
	return p, nil
}

// Validate 校验必填字段。
func (p Persona) Validate() error {
	var errs []error
	if p.Name == "" {
		errs = append(errs, errors.New("人格档案缺少 name"))
	}
	if p.Identity == "" {
		errs = append(errs, errors.New("人格档案缺少 identity"))
	}
	if len(p.Personality) == 0 {
		errs = append(errs, errors.New("人格档案缺少 personality"))
	}
	if len(p.Principles) == 0 {
		errs = append(errs, errors.New("人格档案缺少 principles"))
	}
	return errors.Join(errs...)
}

// normalized 去除空白并复制所有切片，保证调用方持有的副本互不影响。
func (p Persona) normalized() Persona {
	clean := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, s := range in {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return slices.Clip(out)
	}
	return Persona{
		Name:          strings.TrimSpace(p.Name),
		Identity:      strings.TrimSpace(p.Identity),
		Personality:   clean(p.Personality),
		SpeakingStyle: clean(p.SpeakingStyle),
		Catchphrases:  clean(p.Catchphrases),
		Interests:     clean(p.Interests),
		Values:        clean(p.Values),
		Principles:    clean(p.Principles),
	}
}

// WebFile 是网页管理后台保存人格档案的文件名，位于数据目录中。
const WebFile = "persona.yaml"

// 人格档案的来源。
const (
	SourceWeb     = "web"     // 管理后台保存的 <data_dir>/persona.yaml
	SourceFile    = "file"    // 配置文件 persona.file 指定的文件
	SourceBuiltin = "builtin" // 内置的默认人格
)

// Manager 持有当前的人格档案。管理员可以在网页管理后台修改，保存后立即生效；
// 对话中没有任何途径修改它。
type Manager struct {
	configFile string
	webPath    string

	mu       sync.RWMutex
	cur      Persona
	raw      []byte
	source   string
	onChange []func(Persona)
}

// NewManager 按「网页保存的人格 → 配置文件 persona.file → 内置人格」的顺序加载。
func NewManager(configFile, dataDir string) (*Manager, error) {
	m := &Manager{configFile: configFile, webPath: filepath.Join(dataDir, WebFile)}
	if b, err := os.ReadFile(m.webPath); err == nil {
		p, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.webPath, err)
		}
		m.cur, m.raw, m.source = p, b, SourceWeb
		return m, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("读取人格档案: %w", err)
	}
	if err := m.loadFallback(); err != nil {
		return nil, err
	}
	return m, nil
}

// loadFallback 加载配置文件指定的人格，没有指定时使用内置人格。
func (m *Manager) loadFallback() error {
	raw, source := configs.DefaultPersona, SourceBuiltin
	if m.configFile != "" {
		b, err := os.ReadFile(m.configFile)
		if err != nil {
			return fmt.Errorf("读取人格档案: %w", err)
		}
		raw, source = b, SourceFile
	}
	p, err := Parse(raw)
	if err != nil {
		return err
	}
	m.cur, m.raw, m.source = p, raw, source
	return nil
}

// Current 返回当前的人格档案（副本）。
func (m *Manager) Current() Persona {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur.normalized()
}

// Raw 返回当前人格档案的 YAML 原文与来源。
func (m *Manager) Raw() (raw []byte, source, path string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	switch m.source {
	case SourceWeb:
		path = m.webPath
	case SourceFile:
		path = m.configFile
	}
	return slices.Clone(m.raw), m.source, path
}

// OnChange 注册人格变化时的回调。
func (m *Manager) OnChange(fn func(Persona)) {
	m.mu.Lock()
	m.onChange = append(m.onChange, fn)
	m.mu.Unlock()
}

// Save 校验并保存新的人格档案，立即生效。
func (m *Manager) Save(data []byte) (Persona, error) {
	p, err := Parse(data)
	if err != nil {
		return Persona{}, err
	}
	if err := os.MkdirAll(filepath.Dir(m.webPath), 0o750); err != nil {
		return Persona{}, err
	}
	tmp := m.webPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return Persona{}, fmt.Errorf("保存人格档案: %w", err)
	}
	if err := os.Rename(tmp, m.webPath); err != nil {
		return Persona{}, fmt.Errorf("保存人格档案: %w", err)
	}
	m.set(p, data, SourceWeb)
	return p, nil
}

// Reset 删除网页保存的人格档案，恢复为配置文件指定的或内置的人格，立即生效。
func (m *Manager) Reset() (Persona, error) {
	if err := os.Remove(m.webPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Persona{}, err
	}
	fresh := &Manager{configFile: m.configFile}
	if err := fresh.loadFallback(); err != nil {
		return Persona{}, err
	}
	m.set(fresh.cur, fresh.raw, fresh.source)
	return fresh.cur, nil
}

func (m *Manager) set(p Persona, raw []byte, source string) {
	m.mu.Lock()
	m.cur, m.raw, m.source = p, slices.Clone(raw), source
	fns := slices.Clone(m.onChange)
	m.mu.Unlock()
	for _, fn := range fns {
		fn(p.normalized())
	}
}
