//go:build web

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS 返回可嵌入文件系统。
func FS() fs.FS { return distFS }

// Root 是 FS 内的根目录名。
func Root() string { return "dist" }

// HasRealUI 报告是否包含真实前端。
func HasRealUI() bool { return true }
