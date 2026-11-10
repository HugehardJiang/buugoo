package httpapi

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/web"
)

// notBuiltPage 在未内嵌前端时显示，配色取自设计系统。
const notBuiltPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>布谷</title>
<body style="margin:0;background:#faf9f5;color:#141413;font:16px/1.65 -apple-system,BlinkMacSystemFont,'PingFang SC','Microsoft YaHei',sans-serif">
<main style="max-width:560px;margin:14vh auto;padding:0 16px">
<p style="margin:0 0 6px;color:#87867f;font-size:12px;font-weight:700;letter-spacing:.13em">BUUGOO · 布谷</p>
<h1 style="margin:0 0 14px;font:400 36px/1.2 'Iowan Old Style',Georgia,'Noto Serif SC','Songti SC',serif">服务已经启动。</h1>
<p style="margin:0;color:#5e5d59">这个可执行文件没有内嵌 Web 界面。请使用正式发布版本，或在源码目录运行 <code>make web build</code> 后重新启动。也可以用 <code>buugoo chat</code> 在终端里聊天。</p>
</main></body></html>`

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
				if serveGzip(c, fsys, p) {
					return nil
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
		if serveGzip(c, fsys, "index.html") {
			return nil
		}
		return c.HTMLBlob(http.StatusOK, index)
	})
}

// 静态文件压缩：JS、CSS 这类文本文件压缩后小 3～4 倍（没有经过宝塔、nginx 等反向代理时尤其明显）。
// 内嵌的文件不会变，每个文件只压一次，结果放在内存里。

var compressible = map[string]bool{".js": true, ".css": true, ".html": true, ".svg": true, ".json": true, ".txt": true, ".map": true}

var gzipCache sync.Map // 路径 → []byte（压缩后反而不更小的存 nil）

// serveGzip 在浏览器接受 gzip、而且这个文件值得压缩时返回压缩过的内容，返回 false 表示交给普通的文件服务。
func serveGzip(c echo.Context, fsys fs.FS, p string) bool {
	ext := path.Ext(p)
	if !compressible[ext] || !strings.Contains(c.Request().Header.Get("Accept-Encoding"), "gzip") {
		return false
	}
	var gz []byte
	if v, ok := gzipCache.Load(p); ok {
		gz, _ = v.([]byte)
	} else {
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return false
		}
		var buf bytes.Buffer
		w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = w.Write(raw)
		_ = w.Close()
		if buf.Len() < len(raw)*9/10 {
			gz = buf.Bytes()
		}
		gzipCache.Store(p, gz)
	}
	if gz == nil {
		return false
	}
	h := c.Response().Header()
	ctype := mime.TypeByExtension(ext)
	if ext == ".html" {
		ctype = "text/html; charset=utf-8"
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	h.Set("Content-Type", ctype)
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	if c.Request().Method == http.MethodHead {
		c.Response().WriteHeader(http.StatusOK)
		return true
	}
	_ = c.Blob(http.StatusOK, ctype, gz)
	return true
}
