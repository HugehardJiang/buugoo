package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/configs"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/paths"
)

type initOpts struct {
	scope   string
	dataDir string
	mode    string
	listen  string
	force   bool
	stdout  bool
}

func newInitCmd(g *globalOpts) *cobra.Command {
	o := &initOpts{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "生成默认配置文件（已存在则不覆盖）",
		Long: `生成默认配置文件，并自动生成随机的 JWT 密钥。

配置文件位置：--config 指定的路径；否则 root 用户写入系统配置目录，
普通用户写入用户配置目录。已存在的配置文件不会被覆盖（除非使用 --force）。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, g, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.scope, "scope", "", "安装范围 system | user（默认：root 为 system，否则为 user）")
	f.StringVar(&o.dataDir, "data-dir", "", "数据目录（默认按安装范围决定）")
	f.StringVar(&o.mode, "mode", config.ModeSingle, "部署模式 single | multi")
	f.StringVar(&o.listen, "listen", "127.0.0.1:8080", "监听地址")
	f.BoolVar(&o.force, "force", false, "覆盖已存在的配置文件")
	f.BoolVar(&o.stdout, "stdout", false, "只输出到标准输出，不写文件")
	return cmd
}

func runInit(cmd *cobra.Command, g *globalOpts, o *initOpts) error {
	if o.mode != config.ModeSingle && o.mode != config.ModeMulti {
		return fmt.Errorf("--mode 必须是 single 或 multi")
	}
	scope := o.scope
	if scope == "" {
		scope = paths.ScopeUser
		if paths.IsPrivileged() {
			scope = paths.ScopeSystem
		}
	}
	layout, err := paths.Default(scope)
	if err != nil {
		return err
	}
	dataDir := o.dataDir
	if dataDir == "" {
		dataDir = layout.DataDir
	}
	if dataDir, err = filepath.Abs(dataDir); err != nil {
		return err
	}
	secret, err := randomSecret()
	if err != nil {
		return err
	}
	encKey, err := randomSecret()
	if err != nil {
		return err
	}
	content := renderConfig(o.mode, dataDir, o.listen, secret, encKey)

	out := cmd.OutOrStdout()
	if o.stdout {
		_, err := fmt.Fprint(out, content)
		return err
	}

	target := g.configPath
	if target == "" {
		target = layout.ConfigFile()
	}
	if _, err := os.Stat(target); err == nil && !o.force {
		fmt.Fprintf(out, "配置文件已存在，保持不变：%s\n", target)
		return nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("检查配置文件: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("创建数据目录: %w", err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		return fmt.Errorf("写入配置文件: %w", err)
	}
	fmt.Fprintf(out, "已生成配置文件：%s\n数据目录：%s\n", target, dataDir)
	fmt.Fprintln(out, "下一步：在配置文件中填写 llm.chat.api_key 与 llm.embedding.api_key，然后运行 buugoo serve")
	return nil
}

func renderConfig(mode, dataDir, listen, secret, encKey string) string {
	r := strings.NewReplacer(
		"{{MODE}}", mode,
		// YAML 双引号字符串中反斜杠需要转义（Windows 路径）。
		"{{DATA_DIR}}", strings.ReplaceAll(dataDir, `\`, `\\`),
		"{{LISTEN}}", listen,
		"{{JWT_SECRET}}", secret,
		"{{ENC_KEY}}", encKey,
	)
	return r.Replace(configs.ConfigTemplate)
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机密钥: %w", err)
	}
	return hex.EncodeToString(b), nil
}
