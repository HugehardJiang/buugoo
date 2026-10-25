# 宝塔面板：Nginx 反向代理配置指南

目标：用户通过 `https://你的域名` 访问布谷，Nginx（宝塔）负责 HTTPS 与转发，布谷本身只监听本机端口，不直接暴露到公网。

```
浏览器 ──HTTPS──▶ 宝塔 Nginx (443) ──HTTP──▶ 布谷 127.0.0.1:8080
```

## 0. 准备

- 布谷已经安装并在运行（用安装脚本装的：`sudo systemctl status buugoo` 显示 `active (running)`；
  交给宝塔托管的见 README「方式三」）。
- 在服务器上确认布谷能访问：

  ```bash
  curl -s http://127.0.0.1:8080/healthz
  ```

  有返回就说明服务正常。端口以配置文件 `/etc/buugoo/config.yaml` 中的 `server.listen` 为准。
- 域名已经解析到服务器 IP（A 记录）。国内服务器使用 80/443 端口需要域名已备案。

`server.listen` 请保持 `127.0.0.1:8080`：只允许本机的 Nginx 访问。**不要**在宝塔「安全」里放行 8080 端口。

## 1. 添加站点

「网站 → PHP 项目（或「HTML 项目」）→ 添加站点」：

- 域名：填你的域名，例如 `buugoo.example.com`；
- 根目录：保持默认即可（不会用到）；
- FTP、数据库：不创建；
- PHP 版本：选「纯静态」。

## 2. 申请 HTTPS 证书

进入站点设置 →「SSL」→「Let's Encrypt」，勾选域名后申请。成功后打开右上角的「强制 HTTPS」。

申请失败最常见的原因：域名还没有解析到这台服务器，或者 80 端口没有放行（宝塔「安全」和云服务商的安全组都要放行 80、443）。

## 3. 添加反向代理

站点设置 →「反向代理」→「添加反向代理」：

| 项目 | 填写 |
| --- | --- |
| 代理名称 | `buugoo` |
| 目标 URL | `http://127.0.0.1:8080` |
| 发送域名 | `$host` |
| 缓存 | **不要开启** |

保存后，在这条反向代理的「配置文件」（或「操作 → 配置文件」）中，把内容**整个替换**为：

```nginx
location ^~ / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # 聊天回复是流式输出（SSE），必须关闭缓冲和缓存，否则回复会憋到最后一次性出现。
    proxy_buffering off;
    proxy_cache off;
    proxy_request_buffering off;

    # 运行代码、安装 Python 库可能需要几分钟，超时要放长。
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;

    # 上传文件的大小上限，要不小于配置中的 uploads.max_file_mb（默认 20 MB）。
    client_max_body_size 25m;
}
```

几点说明：

- `location ^~ /` 中的 `^~` 不能省：宝塔的站点配置里默认有匹配 `.js`、`.css`、`.png` 等的缓存规则，
  不加 `^~` 时这些请求会被它们拦下，页面会白屏或样式丢失。
- 如果你的宝塔版本不能直接编辑反向代理的配置文件，可以在站点设置 →「配置文件」中，
  把 `location ~ .*\.(gif|jpg|jpeg|png|bmp|swf)$` 和 `location ~ .*\.(js|css)?$` 这两段删除或注释掉，
  再把上面的设置加到反向代理里。
- 布谷已经在流式响应中带了 `X-Accel-Buffering: no`，上面的 `proxy_buffering off` 是双保险。

保存后宝塔会自动检查并重载 Nginx。如果提示配置错误，多半是复制时漏了分号或括号。

### QQ（NapCat）的 WebSocket

NapCat 和布谷在同一台服务器时，NapCat 里直接填 `ws://127.0.0.1:8080/onebot/v11/ws` 即可，不经过 Nginx。
如果 NapCat 在别的机器上、必须经过宝塔，在上面的配置文件里**再加一段**（放在 `location ^~ /` 前面）：

```nginx
location ^~ /onebot/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 3600s;
}
```

NapCat 中的地址填 `wss://你的域名/onebot/v11/ws`。

## 4. 检查

1. 浏览器打开 `https://你的域名`，能看到登录页或聊天页。
2. 发一条消息，回复应该**一个字一个字地出现**；如果憋了很久一下子全出来，说明缓冲没关掉，回到第 3 步检查。
3. 上传一张图片、让布谷画一张图，确认都正常。
4. （multi 模式）管理员在「设置 → 管理后台」或日志中看到的用户 IP 应该是真实 IP，而不是 `127.0.0.1`。

## 常见问题

| 现象 | 原因与处理 |
| --- | --- |
| 502 Bad Gateway | 布谷没在运行或端口不对。`sudo systemctl status buugoo` 查看状态，`journalctl -u buugoo -n 50` 查看日志；确认目标 URL 的端口和 `server.listen` 一致 |
| 504 Gateway Timeout | 超时太短，确认 `proxy_read_timeout 600s` 已生效 |
| 回复一次性出现、没有打字效果 | 开启了缓冲或缓存：确认 `proxy_buffering off`、`proxy_cache off`，并且反向代理的「缓存」没有开启 |
| 上传文件提示 413 | `client_max_body_size` 太小，按第 3 步调大 |
| 页面白屏、样式丢失 | 静态文件被站点里的缓存规则拦下，确认用的是 `location ^~ /` |
| 用户 IP 都是 127.0.0.1 | 缺少 `X-Forwarded-For` 那一行 |
| 申请证书失败 | 域名没解析到这台服务器，或 80 端口没有放行 |
| 改了布谷配置不生效 | 改配置后要重启布谷：`sudo systemctl restart buugoo`（Nginx 不用动） |

## 使用 CDN（可选）

前面再套一层 CDN（如 Cloudflare、各家云的 CDN）时：

- 关闭对 `/api/` 的缓存，并关闭「页面优化」「压缩」之类会缓冲响应的功能，否则流式回复会失效；
- CDN 回源超时调到 600 秒左右（部分 CDN 有上限，超过后长时间运行代码会被断开）。
