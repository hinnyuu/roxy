//go:build !web

package web

import (
	"embed"
	"io/fs"
)

//go:embed placeholder
var placeholderFS embed.FS

// FS 返回可嵌入文件系统。
func FS() fs.FS { return placeholderFS }

// Root 是 FS 内的根目录名。
func Root() string { return "placeholder" }

// HasRealUI 报告是否包含真实前端（占位页为 false）。
func HasRealUI() bool { return false }
