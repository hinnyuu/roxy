package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hinnyuu/roxy/internal/domain"
)

// Handler 消费归一化事件（触发归一化，§3 原则 1）：流水线（parse→match）
// 在 M2 步骤 6 接入；provider 差异对下游不可见。
type Handler func(ctx context.Context, ev domain.SourceEvent) error

// Stats 单次扫描统计（写入 scan 任务 progress）。
type Stats struct {
	Discovered int `json:"discovered"`
	New        int `json:"new"`
	Changed    int `json:"changed"`
	Gone       int `json:"gone"`
	Stale      int `json:"stale"` // 消失但有 placement 引用，留待 M4 巡检
}

// ScanPayload scan 任务的 payload。
type ScanPayload struct {
	SourceID int64 `json:"source_id"`
}

func (p ScanPayload) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// Scanner 把 Provider 的发现结果与台账 diff 成事件。
type Scanner struct {
	store     *Store
	providers map[string]Provider
}

func NewScanner(store *Store) *Scanner {
	s := &Scanner{store: store, providers: map[string]Provider{}}
	s.Register(DirScanProvider{})
	return s
}

func (s *Scanner) Register(p Provider) { s.providers[p.Name()] = p }

// ScanSource 扫描单个源；每个新增/变更文件触发一次 upsert 事件。
func (s *Scanner) ScanSource(ctx context.Context, src domain.Source, h Handler) (*Stats, error) {
	provider, ok := s.providers[src.ProviderType]
	if !ok {
		return nil, fmt.Errorf("scanner: 未注册的 provider %q", src.ProviderType)
	}
	found, err := provider.Discover(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("scanner: discover %s: %w", src.Path, err)
	}
	existing, err := s.store.ExistingFiles(ctx, src.ID)
	if err != nil {
		return nil, err
	}

	stats := &Stats{}
	seen := make(map[string]bool, len(found))
	for _, f := range found {
		seen[f.AbsPath] = true
		stats.Discovered++
		_, changed, err := s.store.UpsertFile(ctx, src.ID, f.AbsPath, f.Size, f.MTime)
		if err != nil {
			return stats, err
		}
		if !changed {
			continue
		}
		if _, exists := existing[f.AbsPath]; exists {
			stats.Changed++
		} else {
			stats.New++
		}
		if h != nil {
			if err := h(ctx, domain.SourceEvent{SourceID: src.ID, AbsPath: f.AbsPath, Op: domain.EventUpsert}); err != nil {
				// 单文件失败不阻断整批（记录后继续，文件状态由流水线置 error）
				slog.Error("scanner: 事件处理失败", "path", f.AbsPath, "err", err)
			}
		}
	}

	for path, row := range existing {
		if seen[path] {
			continue
		}
		stats.Gone++
		removed, err := s.store.ForgetFile(ctx, row.id)
		if err != nil {
			return stats, err
		}
		if !removed {
			stats.Stale++
		}
	}
	return stats, nil
}

// ScanAll 扫描全部启用的源（定时任务入口）。
func (s *Scanner) ScanAll(ctx context.Context, h Handler) error {
	srcs, err := s.store.ListSources(ctx)
	if err != nil {
		return err
	}
	for _, src := range srcs {
		if !src.Enabled {
			continue
		}
		if _, err := s.ScanSource(ctx, src, h); err != nil {
			slog.Error("scanner: 源扫描失败", "source", src.ID, "path", src.Path, "err", err)
		}
	}
	return nil
}

// StartScheduler 定时扫描循环：interval<=0 时不启动（v1 默认仅手动，D-025）。
func StartScheduler(ctx context.Context, interval time.Duration, tick func(context.Context) error) {
	if interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := tick(ctx); err != nil {
					slog.Error("scanner: 定时扫描失败", "err", err)
				}
			}
		}
	}()
}
