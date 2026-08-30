package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hinnyuu/roxy/internal/domain"
	"github.com/hinnyuu/roxy/internal/parser"
)

// Discovered 源发现产出的单个文件事实。
type Discovered struct {
	AbsPath string
	Size    int64
	MTime   time.Time
}

// Provider 源发现抽象（D-025）：v1 dirscan，v2 qbittorrent/transmission。
type Provider interface {
	Name() string
	Discover(ctx context.Context, src domain.Source) ([]Discovered, error)
}

// 未完成的下载临时文件后缀（dirscan 跳过；v2 由客户端事件天然规避）。
var tempExts = []string{".part", ".part1", ".partial", ".downloading", ".!qb", ".crdownload", ".tmp"}

// DirScanProvider 目录扫描（只读遍历，零破坏）。
type DirScanProvider struct{}

func (DirScanProvider) Name() string { return "dirscan" }

func (DirScanProvider) Discover(ctx context.Context, src domain.Source) ([]Discovered, error) {
	var out []Discovered
	err := filepath.WalkDir(src.Path, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != src.Path && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || isTempFile(name) {
			return nil
		}
		kind := parser.Classify(name)
		if kind != parser.KindVideo && kind != parser.KindSubtitle {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			if os.IsNotExist(ierr) {
				return nil // 遍历中文件消失：忽略
			}
			return ierr
		}
		out = append(out, Discovered{AbsPath: path, Size: info.Size(), MTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func isTempFile(name string) bool {
	lower := strings.ToLower(name)
	for _, t := range tempExts {
		if strings.HasSuffix(lower, t) {
			return true
		}
	}
	// qBittorrent 隐藏分片占位（name.### 等以 .! 开头的形态）
	if strings.Contains(lower, ".!") {
		return true
	}
	return false
}
