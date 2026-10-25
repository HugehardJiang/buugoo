# 部署模式与鉴权

## single（自部署默认）

- 启动时自动创建 ID 为 1 的 `owner` 用户（管理员）。
- 不设置 `auth.access_password` 时无需登录；设置后需要输入访问密码，换取 JWT。
- 未配置 `auth.jwt_secret` 时自动生成一个并保存在数据库（`app_meta`），重启后登录仍然有效。
- 默认只监听 `127.0.0.1`；对外监听却没有访问密码时，启动日志会醒目告警。

## multi（官网）

- 用户名 + 密码（bcrypt）注册登录，JWT（HS256）鉴权，`auth.jwt_secret` 必须配置且至少 32 字符。
- 注册方式 `auth.registration`：`open` 开放注册、`invite` 邀请码注册（默认）、`closed` 关闭注册。
- 邀请码：管理员在 Web 设置页或 `buugoo admin invite create` 生成，可设置可用次数和有效期；
  注册时在同一个事务中原子地消耗邀请码并创建用户。
- 第一个管理员用 `buugoo admin create-user <用户名> --admin` 创建。
- 所有数据访问接口都带 `user_id`，Repository 层强制隔离；越权访问统一返回 404。
- `token_version`：修改密码或"退出所有设备"时加一，旧令牌立即失效。
- 登录和注册按 IP 限速（每 6 秒 1 次，可突发 10 次）。部署在反向代理后时信任内网代理设置的 `X-Forwarded-For`。

## 每日用量限额

`quota.daily_messages` 与 `quota.daily_tokens`（0 表示不限），按配置时区的自然日统计；
记忆提取等后台任务消耗的 token 也计入。超出时返回 429 和重置时间。`quota.exempt_admins` 默认为 true。
