package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 工具名。
const (
	NameUse    = "use_skill"
	NameRead   = "read_skill_file"
	NameScript = "run_skill_script"
)

const (
	maxBodyChars = 20000
	maxFileChars = 20000
	maxListed    = 40 // 提示词里最多列出的 skill 数
)

// Tools 返回这个用户能用的 skill 工具。runner 为空时不能运行 skill 的脚本（只能读说明）。
func Tools(m *Manager, list []store.Skill, runner tools.Runner, ws tools.Workspace) []tools.Extra {
	if m == nil || len(list) == 0 {
		return nil
	}
	byName := map[string]store.Skill{}
	var hint strings.Builder
	hint.WriteString("use_skill / read_skill_file / run_skill_script：已安装的 skill 是现成的做事方法。对方的要求和下面某个 skill 的说明对得上时，" +
		"先用 use_skill 读它的完整说明，再照着做；不相关时不要用。可用的 skill：")
	for i, s := range list {
		byName[s.Name] = s
		if i < maxListed {
			fmt.Fprintf(&hint, "\n  - %s：%s", s.Name, clip(s.Description, 300))
		}
	}
	names := make([]string, 0, len(list))
	for _, s := range list {
		names = append(names, s.Name)
	}
	nameProp := map[string]any{"type": "string", "enum": names, "description": "skill 的名字"}
	find := func(name string) (store.Skill, bool) {
		s, ok := byName[strings.TrimSpace(name)]
		return s, ok
	}

	out := []tools.Extra{
		{
			Def: llm.Tool{
				Name:        NameUse,
				Description: "读取一个 skill 的完整使用说明（SKILL.md）和它带的文件列表。用 skill 之前先读。",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{"name": nameProp}, "required": []string{"name"}},
			},
			Label: "使用 skill",
			Hint:  hint.String(),
			Run: func(_ context.Context, args json.RawMessage) tools.Result {
				var in struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(args, &in)
				res := tools.Result{Input: in.Name}
				s, ok := find(in.Name)
				if !ok {
					res.Output, res.Summary = "没有这个 skill", "没有这个 skill"
					return res
				}
				body, err := m.Body(s.Name)
				if err != nil {
					res.Output, res.Summary = "读取说明失败："+err.Error(), "读取失败"
					return res
				}
				var b strings.Builder
				fmt.Fprintf(&b, "# skill：%s\n%s\n\n%s", s.Name, s.Description, clip(body, maxBodyChars))
				if files := m.Files(s.Name, 80); len(files) > 0 {
					b.WriteString("\n\n## 这个 skill 带的文件\n" + strings.Join(files, "\n"))
					b.WriteString("\n\n需要看文件内容时用 read_skill_file。")
				}
				if runner != nil {
					b.WriteString("\n说明里要运行脚本或命令时，用 run_skill_script（在这个 skill 的文件夹里执行，例如 python scripts/xxx.py 参数）。")
					if s.AllowNetwork {
						b.WriteString("这个 skill 的脚本可以联网。")
					} else {
						b.WriteString("这个 skill 的脚本不能联网。")
					}
				} else {
					b.WriteString("\n现在不能运行代码：说明里要运行脚本的步骤做不了，告诉对方。")
				}
				res.OK, res.Summary, res.Output = true, "读了 "+s.Name+" 的说明", b.String()
				return res
			},
		},
		{
			Def: llm.Tool{
				Name:        NameRead,
				Description: "读取 skill 里的一个文件（参考资料、模板、脚本源码）。",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{
					"name": nameProp,
					"path": map[string]any{"type": "string", "description": "文件在 skill 里的路径，例如 references/api.md"},
				}, "required": []string{"name", "path"}},
			},
			Label: "读 skill 文件",
			Run: func(_ context.Context, args json.RawMessage) tools.Result {
				var in struct {
					Name string `json:"name"`
					Path string `json:"path"`
				}
				_ = json.Unmarshal(args, &in)
				res := tools.Result{Input: in.Name + "/" + in.Path}
				s, ok := find(in.Name)
				if !ok {
					res.Output, res.Summary = "没有这个 skill", "没有这个 skill"
					return res
				}
				b, err := m.ReadFile(s.Name, in.Path)
				if err != nil {
					res.Output, res.Summary = err.Error(), "读取失败"
					return res
				}
				if !utf8.Valid(b) {
					res.Output, res.Summary = fmt.Sprintf("%s 不是文本文件（%d 字节），可以在 run_skill_script 里用到它", in.Path, len(b)), "不是文本文件"
					return res
				}
				res.OK, res.Summary, res.Output = true, "读了 "+in.Path, clip(string(b), maxFileChars)
				return res
			},
		},
	}
	if runner != nil {
		out = append(out, tools.Extra{
			Def: llm.Tool{
				Name: NameScript,
				Description: "按 skill 的说明运行它带的脚本或命令：在这个 skill 的文件夹里执行（只读），用 sh 运行，可以带参数，例如 python scripts/weather.py 北京。" +
					"要保存文件时写到环境变量 WORK_DIR 指的目录（这段对话的工作目录），图片会自动显示给对方。只有允许联网的 skill 能联网。",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{
					"name":    nameProp,
					"command": map[string]any{"type": "string", "description": "要运行的命令"},
				}, "required": []string{"name", "command"}},
			},
			Label: "运行 skill",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var in struct {
					Name    string `json:"name"`
					Command string `json:"command"`
				}
				_ = json.Unmarshal(args, &in)
				s, ok := find(in.Name)
				if !ok {
					return tools.Result{Input: in.Command, Output: "没有这个 skill", Summary: "没有这个 skill"}
				}
				if strings.TrimSpace(in.Command) == "" || len(in.Command) > 4000 {
					return tools.Result{Input: in.Command, Output: "命令为空或太长", Summary: "命令不对"}
				}
				res := tools.RunCode(ctx, runner, ws, scriptCode(s.Name, in.Command), func(env *tools.RunEnv) {
					env.Skills, env.Network = m.Dir(), s.AllowNetwork
				})
				res.Input = s.Name + "：" + in.Command
				return res
			},
		})
	}
	return out
}

// scriptCode 是在沙箱里运行 skill 命令的 Python 代码：在 skill 文件夹里用 sh 执行，输出原样打印。
func scriptCode(name, command string) string {
	return fmt.Sprintf(`import os, subprocess, sys
skill = os.path.join(os.environ.get("BUUGOO_SKILLS", ""), %s)
env = dict(os.environ, SKILL_DIR=skill, WORK_DIR=os.getcwd())
r = subprocess.run(%s, shell=True, cwd=skill, env=env, capture_output=True, text=True)
sys.stdout.write(r.stdout)
sys.stderr.write(r.stderr)
sys.exit(r.returncode)
`, strconv.Quote(name), strconv.Quote(command))
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…（后面省略）"
	}
	return s
}
