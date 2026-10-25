package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/HugehardJiang/buugoo/internal/client"
	"github.com/HugehardJiang/buugoo/internal/paths"
	"github.com/HugehardJiang/buugoo/internal/tui"
)

type chatOpts struct {
	server  string
	message string
	logout  bool
	plain   bool
}

func newChatCmd(g *globalOpts) *cobra.Command {
	o := &chatOpts{}
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "在终端和布谷聊天（连接本地或远程服务）",
		Long: `在终端和布谷聊天。

服务地址按以下顺序确定：--server → 环境变量 BUUGOO_SERVER → 配置文件中的监听地址 → http://127.0.0.1:8080。
需要登录时会提示输入密码，登录令牌保存在用户配置目录的 credentials.json 中。

非交互用法：
  buugoo chat -m "今天好累"        发送一条消息并输出回复
  echo "你好" | buugoo chat         从标准输入逐行发送`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runChat(cmd, g, o) },
	}
	f := cmd.Flags()
	f.StringVarP(&o.server, "server", "s", "", "服务地址，例如 https://buugoo.example.com")
	f.StringVarP(&o.message, "message", "m", "", "发送一条消息并输出回复后退出")
	f.BoolVar(&o.logout, "logout", false, "删除该服务器保存的登录令牌")
	f.BoolVar(&o.plain, "plain", false, "使用纯文本模式（不启动全屏界面）")
	return cmd
}

func resolveServer(g *globalOpts, flag string) string {
	if flag != "" {
		return strings.TrimRight(flag, "/")
	}
	if env := os.Getenv("BUUGOO_SERVER"); env != "" {
		return strings.TrimRight(env, "/")
	}
	if cfg, err := loadConfig(g); err == nil {
		host, port, err := net.SplitHostPort(cfg.Server.Listen)
		if err == nil {
			if host == "" || host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			return "http://" + net.JoinHostPort(host, port)
		}
	}
	return "http://127.0.0.1:8080"
}

func credentialsPath() (string, error) {
	l, err := paths.Default(paths.ScopeUser)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.ConfigDir, "credentials.json"), nil
}

func runChat(cmd *cobra.Command, g *globalOpts, o *chatOpts) error {
	ctx := cmd.Context()
	server := resolveServer(g, o.server)
	credPath, err := credentialsPath()
	if err != nil {
		return err
	}
	creds, err := client.LoadCredentials(credPath)
	if err != nil {
		return err
	}
	if o.logout {
		if err := creds.Set(server, ""); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "已删除 %s 的登录令牌\n", server)
		return nil
	}

	c := client.New(server, creds.Get(server))
	meta, err := c.Meta(ctx)
	if err != nil {
		return err
	}
	if meta.PasswordRequired {
		if _, err := c.Me(ctx); err != nil {
			if !client.IsUnauthorized(err) {
				return err
			}
			if err := interactiveLogin(cmd, c, meta); err != nil {
				return err
			}
			if err := creds.Set(server, c.Token); err != nil {
				return err
			}
		}
	}
	if ok, err := c.HasModel(ctx); err == nil && !ok {
		fmt.Fprintln(cmd.ErrOrStderr(), "提示：还没有可用的聊天模型，请先在网页「设置 → 模型」中添加，或在配置文件 llm.chat 中填写。")
	}

	stdinTTY := isTerminal(os.Stdin)
	stdoutTTY := isTerminal(os.Stdout)
	switch {
	case o.message != "":
		return plainSend(cmd, c, meta.Name, o.message, stdoutTTY)
	case o.plain || !stdinTTY || !stdoutTTY:
		return plainLoop(cmd, c, meta.Name, stdinTTY && stdoutTTY)
	}
	style := "dark"
	if !lipgloss.HasDarkBackground() {
		style = "light"
	}
	return tui.Run(ctx, tui.Options{Client: c, Name: meta.Name, Server: server, GlamourStyle: style})
}

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func interactiveLogin(cmd *cobra.Command, c *client.Client, meta *client.Meta) error {
	if !isTerminal(os.Stdin) {
		return errors.New("需要登录，请先在终端中交互运行 buugoo chat 完成登录")
	}
	errOut := cmd.ErrOrStderr()
	username := ""
	if meta.Mode == "multi" {
		fmt.Fprintf(errOut, "登录 %s\n用户名：", c.BaseURL)
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return fmt.Errorf("读取用户名: %w", err)
		}
		username = strings.TrimSpace(line)
		fmt.Fprint(errOut, "密码：")
	} else {
		fmt.Fprintf(errOut, "%s 需要访问密码：", c.BaseURL)
	}
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(errOut)
	if err != nil {
		return fmt.Errorf("读取密码: %w", err)
	}
	if _, err := c.Login(cmd.Context(), username, string(pw)); err != nil {
		return err
	}
	return nil
}

// plainSend 发送一条消息，把回复流式写到标准输出。
func plainSend(cmd *cobra.Command, c *client.Client, name, text string, decorate bool) error {
	out := cmd.OutOrStdout()
	if decorate {
		fmt.Fprintf(out, "%s：", name)
	}
	_, err := c.Chat(cmd.Context(), text, func(ev client.StreamEvent) {
		if ev.Type == "delta" {
			fmt.Fprint(out, ev.Text)
		}
	})
	fmt.Fprintln(out)
	return err
}

// plainLoop 逐行读取标准输入并发送。
func plainLoop(cmd *cobra.Command, c *client.Client, name string, interactive bool) error {
	r := bufio.NewReader(cmd.InOrStdin())
	for {
		if interactive {
			fmt.Fprint(cmd.OutOrStdout(), "你：")
		}
		line, err := r.ReadString('\n')
		text := strings.TrimSpace(line)
		if text != "" {
			if text == "/quit" || text == "/exit" {
				return nil
			}
			if serr := plainSend(cmd, c, name, text, interactive); serr != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "错误:", serr)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
