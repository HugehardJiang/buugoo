// Package settings 支持在网页管理后台查看与修改系统设置。
//
// 设置项通过反射从 config.Config 的 koanf 标签生成，修改后写入数据目录中的 settings.yaml
// （覆盖配置文件，环境变量仍然优先），重启服务后生效。管理员密码与加密密钥等只能在配置文件中修改。
package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/HugehardJiang/buugoo/internal/config"
)

// Field 是一个设置项。
type Field struct {
	Key     string   `json:"key"`
	Group   string   `json:"group"`
	Label   string   `json:"label"`
	Help    string   `json:"help,omitempty"`
	Type    string   `json:"type"` // string | bool | int | float | duration | list | select | secret
	Options []string `json:"options,omitempty"`
	// Value 是当前生效（正在运行）的值；密钥类只给出是否已设置。
	Value any `json:"value"`
	// Saved 是网页设置文件中保存的值（与运行中不同表示待重启生效）。
	Saved any `json:"saved,omitempty"`
	// Pending 表示已保存但还没有生效（需要重启）。
	Pending bool `json:"pending"`
	// Locked 表示只能在配置文件中修改；Env 表示被环境变量覆盖，网页修改不会生效。
	Locked bool `json:"locked"`
	Env    bool `json:"env"`
}

// locked 是不能在网页上修改的键：改错会无法登录、已保存的 Key 无法解密，或找不到数据。
var locked = map[string]string{
	"admin.password":          "只能在配置文件中修改",
	"security.encryption_key": "修改后已保存的 API Key 无法解密，只能在配置文件中修改",
	"data_dir":                "只能在配置文件中修改",
}

func isSecret(key string) bool {
	for _, s := range []string{"api_key", "password", "secret", "dsn", "encryption_key"} {
		if strings.HasSuffix(key, s) {
			return true
		}
	}
	return false
}

// walk 遍历配置结构体的叶子字段。
func walk(v reflect.Value, prefix string, fn func(key string, f reflect.Value)) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("koanf")
		if tag == "" || !sf.IsExported() {
			continue
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Struct && fv.Type() != reflect.TypeOf(time.Time{}) {
			walk(fv, key, fn)
			continue
		}
		fn(key, fv)
	}
}

var durationType = reflect.TypeOf(time.Duration(0))

func fieldType(key string, f reflect.Value) string {
	switch {
	case isSecret(key):
		return "secret"
	case f.Type() == durationType:
		return "duration"
	}
	if m, ok := meta[key]; ok && len(m.options) > 0 {
		return "select"
	}
	switch f.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int64, reflect.Int32:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Slice:
		return "list"
	}
	return "string"
}

// display 返回字段在网页上展示的值。
func display(key string, f reflect.Value) any {
	if isSecret(key) {
		return f.String() != ""
	}
	switch {
	case f.Type() == durationType:
		return time.Duration(f.Int()).String()
	case f.Kind() == reflect.Slice:
		out := make([]string, f.Len())
		for i := range out {
			out[i] = fmt.Sprint(f.Index(i).Interface())
		}
		return out
	case f.Kind() == reflect.Float32:
		return float64(float32(f.Float()))
	}
	return f.Interface()
}

// Fields 返回全部设置项。cfg 是正在运行的配置。
func Fields(cfg *config.Config) ([]Field, error) {
	saved, err := readSaved(cfg.SettingsPath())
	if err != nil {
		return nil, err
	}
	var out []Field
	walk(reflect.ValueOf(cfg).Elem(), "", func(key string, f reflect.Value) {
		m := meta[key]
		fd := Field{Key: key, Group: groupOf(key), Label: m.label, Help: m.help, Type: fieldType(key, f),
			Options: m.options, Value: display(key, f), Env: cfg.FromEnv(key)}
		if fd.Label == "" {
			fd.Label = key
		}
		if why, ok := locked[key]; ok {
			fd.Locked = true
			fd.Help = strings.TrimSpace(fd.Help + " " + why)
		}
		if sv, ok := saved[key]; ok {
			if isSecret(key) {
				fd.Saved = sv != ""
				// 密钥只能比较是否已设置之外的内容：保存的值与运行中的不同即待生效。
				fd.Pending = fmt.Sprint(sv) != f.String()
			} else {
				fd.Saved = sv
				fd.Pending = !sameValue(f, sv)
			}
		}
		out = append(out, fd)
	})
	return out, nil
}

func groupOf(key string) string {
	top := strings.SplitN(key, ".", 2)[0]
	switch key {
	case "mode", "data_dir", "timezone":
		top = "general"
	}
	return top
}

// sameValue 比较运行中的值与保存的值。
func sameValue(f reflect.Value, saved any) bool {
	cur := reflect.New(f.Type()).Elem()
	cur.Set(f)
	probe := reflect.New(f.Type()).Elem()
	if err := assign(probe, saved); err != nil {
		return false
	}
	return reflect.DeepEqual(cur.Interface(), probe.Interface())
}

// assign 把网页传来的值转换为字段类型后写入。
func assign(f reflect.Value, v any) error {
	switch {
	case f.Type() == durationType:
		d, err := time.ParseDuration(strings.TrimSpace(fmt.Sprint(v)))
		if err != nil {
			return fmt.Errorf("时长格式不对（例如 30s、5m、2h）")
		}
		f.SetInt(int64(d))
		return nil
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(strings.TrimSpace(fmt.Sprint(v)))
	case reflect.Bool:
		switch x := v.(type) {
		case bool:
			f.SetBool(x)
		default:
			b, err := strconv.ParseBool(fmt.Sprint(v))
			if err != nil {
				return fmt.Errorf("需要 true 或 false")
			}
			f.SetBool(b)
		}
	case reflect.Int, reflect.Int64, reflect.Int32:
		n, err := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(number(v))), 10, 64)
		if err != nil {
			return fmt.Errorf("需要整数")
		}
		f.SetInt(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(number(v))), 64)
		if err != nil {
			return fmt.Errorf("需要数字")
		}
		f.SetFloat(n)
	case reflect.Slice:
		var items []string
		switch x := v.(type) {
		case []any:
			for _, it := range x {
				items = append(items, strings.TrimSpace(fmt.Sprint(it)))
			}
		case []string:
			items = x
		default:
			for _, it := range strings.Split(fmt.Sprint(v), ",") {
				if it = strings.TrimSpace(it); it != "" {
					items = append(items, it)
				}
			}
		}
		sl := reflect.MakeSlice(f.Type(), 0, len(items))
		for _, it := range items {
			if it != "" {
				sl = reflect.Append(sl, reflect.ValueOf(it))
			}
		}
		f.Set(sl)
	default:
		return fmt.Errorf("不支持的类型")
	}
	return nil
}

// number 把 JSON 中的整数形式的浮点数（例如 20.0）转换为整数文本。
func number(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}

// yamlValue 返回写入 settings.yaml 的值。
func yamlValue(f reflect.Value) any {
	switch {
	case f.Type() == durationType:
		return time.Duration(f.Int()).String()
	case f.Kind() == reflect.Float32:
		return float64(float32(f.Float()))
	}
	return f.Interface()
}

// ValidationError 是设置校验错误。
type ValidationError struct {
	Key string
	Msg string
}

func (e *ValidationError) Error() string {
	if e.Key == "" {
		return e.Msg
	}
	return e.Key + "：" + e.Msg
}

// Save 校验并保存修改。changes 的键是设置项（如 llm.chat.model），密钥类留空表示不修改。
// 保存的内容与 settings.yaml 中已有的设置合并；值与配置文件相同时也照常保存，以网页设置为准。
// 返回保存后需要重启才生效的键。
func Save(cfg *config.Config, changes map[string]any) ([]string, error) {
	saved, err := readSaved(cfg.SettingsPath())
	if err != nil {
		return nil, err
	}
	// 在运行中配置的副本上应用「已保存 + 本次修改」，再整体校验。
	candidate := *cfg
	cv := reflect.ValueOf(&candidate).Elem()
	fields := map[string]reflect.Value{}
	walk(cv, "", func(key string, f reflect.Value) { fields[key] = f })
	for key, v := range saved {
		if f, ok := fields[key]; ok {
			_ = assign(f, v)
		}
	}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var changed []string
	for _, key := range keys {
		v := changes[key]
		f, ok := fields[key]
		if !ok {
			return nil, &ValidationError{key, "没有这个设置项"}
		}
		if _, ok := locked[key]; ok {
			return nil, &ValidationError{key, locked[key]}
		}
		if isSecret(key) && strings.TrimSpace(fmt.Sprint(v)) == "" {
			continue // 密钥留空表示不修改
		}
		if m, ok := meta[key]; ok && len(m.options) > 0 && !contains(m.options, fmt.Sprint(v)) {
			return nil, &ValidationError{key, "可选值为 " + strings.Join(m.options, "、")}
		}
		if err := assign(f, v); err != nil {
			return nil, &ValidationError{key, err.Error()}
		}
		saved[key] = yamlValue(f)
		changed = append(changed, key)
	}
	if err := candidate.Validate(); err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}
	if err := writeSaved(cfg.SettingsPath(), saved); err != nil {
		return nil, err
	}
	return changed, nil
}

// Discard 删除网页保存的某些设置（恢复为配置文件中的值）；keys 为空时删除全部。
func Discard(cfg *config.Config, keys []string) error {
	saved, err := readSaved(cfg.SettingsPath())
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		saved = map[string]any{}
	}
	for _, k := range keys {
		delete(saved, k)
	}
	return writeSaved(cfg.SettingsPath(), saved)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// readSaved 读取 settings.yaml，返回扁平的「键 → 值」。
func readSaved(path string) (map[string]any, error) {
	out := map[string]any{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取网页设置: %w", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(b, &tree); err != nil {
		return nil, fmt.Errorf("解析网页设置 %s: %w", path, err)
	}
	flatten("", tree, out)
	return out, nil
}

func flatten(prefix string, m map[string]any, out map[string]any) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			flatten(key, sub, out)
			continue
		}
		out[key] = v
	}
}

// writeSaved 把扁平的设置写回嵌套的 YAML（原子替换，权限 0600：其中可能有密钥）。
func writeSaved(path string, flat map[string]any) error {
	tree := map[string]any{}
	for key, v := range flat {
		parts := strings.Split(key, ".")
		node := tree
		for _, p := range parts[:len(parts)-1] {
			next, ok := node[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[p] = next
			}
			node = next
		}
		node[parts[len(parts)-1]] = v
	}
	b, err := yaml.Marshal(tree)
	if err != nil {
		return err
	}
	header := "# 由布谷网页管理后台生成，覆盖配置文件中的同名设置（环境变量仍然优先）。\n# 重启服务后生效。可以手动编辑，删除本文件即恢复为配置文件中的设置。\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(header), b...), 0o600); err != nil {
		return fmt.Errorf("保存网页设置: %w", err)
	}
	return os.Rename(tmp, path)
}
