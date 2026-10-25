package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/HugehardJiang/buugoo/internal/auth"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func newAdminCmd(g *globalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "管理命令（用户、邀请码），直接操作数据库",
	}
	cmd.AddCommand(newAdminCreateUserCmd(g), newAdminSetAdminCmd(g), newAdminUsersCmd(g), newAdminInviteCmd(g))
	return cmd
}

// withAuth 打开数据库（自动执行迁移）并构造鉴权服务。
func withAuth(cmd *cobra.Command, g *globalOpts, fn func(ctx context.Context, cfg *config.Config, db *gormstore.DB, svc *auth.Service) error) error {
	cfg, err := loadConfig(g)
	if err != nil {
		return err
	}
	log, err := newLogger(cfg)
	if err != nil {
		return err
	}
	db, err := gormstore.Open(cfg.Database, log)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := cmd.Context()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	svc, err := auth.NewService(ctx, cfg, db)
	if err != nil {
		return err
	}
	return fn(ctx, cfg, db, svc)
}

func newAdminCreateUserCmd(g *globalOpts) *cobra.Command {
	var admin, passwordStdin bool
	cmd := &cobra.Command{
		Use:   "create-user <用户名>",
		Short: "创建用户（multi 模式）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withAuth(cmd, g, func(ctx context.Context, cfg *config.Config, _ *gormstore.DB, svc *auth.Service) error {
				if cfg.Mode != config.ModeMulti {
					return errors.New("single 模式无需创建用户")
				}
				pw, err := readPassword(cmd, passwordStdin)
				if err != nil {
					return err
				}
				u, err := svc.CreateUser(ctx, args[0], pw, admin)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "已创建用户 %s（id=%d，管理员=%v）\n", u.Username, u.ID, u.IsAdmin)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&admin, "admin", false, "设为管理员")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "从标准输入读取密码")
	return cmd
}

func newAdminSetAdminCmd(g *globalOpts) *cobra.Command {
	var revoke bool
	cmd := &cobra.Command{
		Use:   "set-admin <用户名>",
		Short: "设置或取消管理员",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withAuth(cmd, g, func(ctx context.Context, _ *config.Config, db *gormstore.DB, _ *auth.Service) error {
				u, err := db.Users().GetByUsername(ctx, args[0])
				if err != nil {
					return fmt.Errorf("查找用户 %s: %w", args[0], err)
				}
				if err := db.Users().SetAdmin(ctx, u.ID, !revoke); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "用户 %s 管理员=%v\n", u.Username, !revoke)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "取消管理员")
	return cmd
}

func newAdminUsersCmd(g *globalOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "users",
		Short: "列出用户",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withAuth(cmd, g, func(ctx context.Context, _ *config.Config, db *gormstore.DB, _ *auth.Service) error {
				us, err := db.Users().List(ctx, 1000, 0)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "ID\t用户名\t管理员\t注册时间")
				for _, u := range us {
					fmt.Fprintf(w, "%d\t%s\t%v\t%s\n", u.ID, u.Username, u.IsAdmin, u.CreatedAt.Local().Format(time.DateTime))
				}
				return w.Flush()
			})
		},
	}
}

func newAdminInviteCmd(g *globalOpts) *cobra.Command {
	cmd := &cobra.Command{Use: "invite", Short: "管理邀请码"}
	var in auth.InviteInput
	var count int
	create := &cobra.Command{
		Use:   "create",
		Short: "生成邀请码",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withAuth(cmd, g, func(ctx context.Context, _ *config.Config, _ *gormstore.DB, svc *auth.Service) error {
				for i := 0; i < count; i++ {
					inv, err := svc.CreateInvite(ctx, 0, in)
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), inv.Code)
				}
				return nil
			})
		},
	}
	create.Flags().IntVar(&in.MaxUses, "uses", 1, "每个邀请码可用次数（0 表示不限）")
	create.Flags().DurationVar(&in.TTL, "ttl", 7*24*time.Hour, "有效期（0 表示永不过期）")
	create.Flags().StringVar(&in.Note, "note", "", "备注")
	create.Flags().IntVarP(&count, "count", "n", 1, "生成数量")

	list := &cobra.Command{
		Use:   "list",
		Short: "列出邀请码",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withAuth(cmd, g, func(ctx context.Context, _ *config.Config, db *gormstore.DB, _ *auth.Service) error {
				invs, err := db.Invites().List(ctx, 1000, 0)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "邀请码\t已用/上限\t过期时间\t备注")
				for _, inv := range invs {
					limit, exp := "不限", "永不"
					if inv.MaxUses > 0 {
						limit = fmt.Sprint(inv.MaxUses)
					}
					if inv.ExpiresAt != nil {
						exp = inv.ExpiresAt.Local().Format(time.DateTime)
					}
					fmt.Fprintf(w, "%s\t%d/%s\t%s\t%s\n", inv.Code, inv.UsedCount, limit, exp, inv.Note)
				}
				return w.Flush()
			})
		},
	}
	cmd.AddCommand(create, list)
	return cmd
}

// readPassword 从标准输入读取密码；终端下不回显并要求输入两次。
func readPassword(cmd *cobra.Command, fromStdin bool) (string, error) {
	in := cmd.InOrStdin()
	f, isFile := in.(*os.File)
	if fromStdin || !isFile || !term.IsTerminal(int(f.Fd())) {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("读取密码: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	errOut := cmd.ErrOrStderr()
	fmt.Fprint(errOut, "密码：")
	p1, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(errOut)
	if err != nil {
		return "", fmt.Errorf("读取密码: %w", err)
	}
	fmt.Fprint(errOut, "再次输入：")
	p2, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(errOut)
	if err != nil {
		return "", fmt.Errorf("读取密码: %w", err)
	}
	if string(p1) != string(p2) {
		return "", errors.New("两次输入的密码不一致")
	}
	return string(p1), nil
}
