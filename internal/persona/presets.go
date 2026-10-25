package persona

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/HugehardJiang/buugoo/configs"
)

// Preset 是用户可以选的预设性格。它只替换人格档案中的自我认知、性格、说话风格和口头禅，
// 名字、兴趣、价值观和必须遵守的原则仍然使用管理后台设置的人格档案。
type Preset struct {
	ID            string   `yaml:"id" json:"id"`
	Label         string   `yaml:"label" json:"label"`
	Summary       string   `yaml:"summary" json:"summary"`
	Identity      string   `yaml:"identity" json:"-"`
	Personality   []string `yaml:"personality" json:"-"`
	SpeakingStyle []string `yaml:"speaking_style" json:"-"`
	Catchphrases  []string `yaml:"catchphrases" json:"catchphrases"`
}

// Apply 返回套用预设后的人格档案。
func (p Preset) Apply(base Persona) Persona {
	out := base.normalized()
	out.Identity = strings.ReplaceAll(p.Identity, "{name}", out.Name)
	out.Personality = slices.Clone(p.Personality)
	out.SpeakingStyle = slices.Clone(p.SpeakingStyle)
	out.Catchphrases = slices.Clone(p.Catchphrases)
	return out
}

var (
	presetsOnce sync.Once
	presets     []Preset
	presetsErr  error
)

// parsePresets 解析内置的预设性格，并检查必填字段与 ID 是否重复。
func parsePresets(data []byte) ([]Preset, error) {
	var list []Preset
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("解析预设性格: %w", err)
	}
	seen := map[string]bool{}
	for _, p := range list {
		switch {
		case p.ID == "" || p.Label == "" || p.Summary == "" || p.Identity == "" || len(p.Personality) == 0 || len(p.SpeakingStyle) == 0:
			return nil, fmt.Errorf("预设性格 %q 缺少必填字段", p.ID)
		case seen[p.ID] || p.ID == DefaultPreset:
			return nil, fmt.Errorf("预设性格 ID %q 重复", p.ID)
		}
		seen[p.ID] = true
	}
	return list, nil
}

// DefaultPreset 是「默认」那一项的 ID：使用管理后台设置的人格档案本身。
const DefaultPreset = ""

// Presets 返回内置的全部预设性格（不含默认）。
func Presets() []Preset {
	presetsOnce.Do(func() { presets, presetsErr = parsePresets(configs.PersonaPresets) })
	if presetsErr != nil {
		// 内置文件由测试保证正确，出错说明打包有问题。
		panic(presetsErr)
	}
	return slices.Clone(presets)
}

// Available 返回没有被管理员关掉的预设性格。
func Available(disabled []string) []Preset {
	return slices.DeleteFunc(Presets(), func(p Preset) bool { return slices.Contains(disabled, p.ID) })
}

// Find 返回可用的预设性格；id 为默认、不存在或已被关掉时返回 nil（使用默认人格）。
func Find(id string, disabled []string) *Preset {
	if id == DefaultPreset {
		return nil
	}
	for _, p := range Available(disabled) {
		if p.ID == id {
			return &p
		}
	}
	return nil
}
