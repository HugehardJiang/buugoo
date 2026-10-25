// Package web 内嵌前端构建产物（由 make web 复制到 dist/）。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS 返回前端静态文件系统。
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// dist 目录在编译期必然存在（含 .gitkeep），这里不会出错。
		panic(err)
	}
	return sub
}

// Built 报告是否已内嵌前端构建产物。
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
