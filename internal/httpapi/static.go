package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/web"
)

const notBuiltPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>布谷</title>
<body style="font-family:sans-serif;max-width:40em;margin:4em auto;line-height:1.7">
<h1>布谷服务已启动</h1><p>当前可执行文件未内嵌 Web 界面。请使用正式发布版本，或在源码目录执行 <code>make web build</code> 后重新运行。</p>
<p>API 位于 <code>/api/v1</code>，也可以使用 <code>buugoo chat</code> 在终端聊天。</p></body></html>`

// mountWeb 挂载内嵌的前端：静态资源直接返回，其余路径回退到 index.html（前端路由）。
func (s *server) mountWeb(e *echo.Echo) {
	fsys := web.FS()
	built := web.Built()
	fileServer := http.FileServer(http.FS(fsys))
	e.GET("/*", func(c echo.Context) error {
		if !built {
			return c.HTML(http.StatusOK, notBuiltPage)
		}
		p := strings.TrimPrefix(path.Clean(c.Request().URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(fsys, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					// Vite 产物文件名带内容哈希，可以长期缓存。
					c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(c.Response(), c.Request())
				return nil
			}
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			return err
		}
		c.Response().Header().Set("Cache-Control", "no-cache")
		return c.HTMLBlob(http.StatusOK, index)
	})
}
