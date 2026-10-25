// Package persona 加载人格档案。人格档案只在启动时读取一次，没有任何接口可以在对话中修改它。
package persona

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

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
	Principles    []string `yaml:"principles"`
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

// Parse 解析并校验人格档案。未知字段视为错误，避免拼写错误被静默忽略。
func Parse(data []byte) (Persona, error) {
	var p Persona
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return Persona{}, fmt.Errorf("解析人格档案: %w", err)
	}
	p = p.normalized()
	if err := p.Validate(); err != nil {
		return Persona{}, err
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
