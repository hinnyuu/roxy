package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hinnyuu/roxy/internal/api"
	"github.com/hinnyuu/roxy/internal/config"
	"github.com/hinnyuu/roxy/internal/matcher"
	"github.com/hinnyuu/roxy/internal/metadata"
	"github.com/hinnyuu/roxy/internal/parser"
	"github.com/hinnyuu/roxy/internal/review"
	"github.com/hinnyuu/roxy/internal/scanner"
	"github.com/hinnyuu/roxy/internal/task"
)

// buildDeps 组装 M2 流水线依赖并启动任务 worker / 定时扫描 / 每周索引自动刷新。
func buildDeps(ctx context.Context, cfg *config.Config, database *sql.DB) (api.Deps, *task.Runner, error) {
	store := scanner.NewStore(database)
	idx := metadata.NewIndex(database)
	cache := metadata.NewCache(database)
	importer := metadata.NewImporter(database, cfg.DataDir, cfg.Metadata.Bangumi.UserAgent)

	var client *metadata.Client
	if cfg.Metadata.Bangumi.Enabled {
		token := ""
		if cfg.Metadata.Bangumi.TokenEnv != "" {
			token = os.Getenv(cfg.Metadata.Bangumi.TokenEnv)
		}
		client = metadata.NewClient("https://api.bgm.tv", cfg.Metadata.Bangumi.UserAgent, token)
	}

	mp := matcher.New(database, parser.New(nil), idx, client, cache,
		cfg.Policy.AutoApproveThreshold, cfg.Policy.MultiVersion, cfg.Policy.SeriesFirstConfirm)
	sc := scanner.NewScanner(store)
	runner := task.NewRunner(database)

	runner.Register("scan", func(ctx context.Context, payload string, report task.Report) error {
		var p scanner.ScanPayload
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return fmt.Errorf("scan payload: %w", err)
		}
		src, err := store.GetSource(ctx, p.SourceID)
		if err != nil {
			return err
		}
		stats, serr := sc.ScanSource(ctx, *src, mp.ProcessEvent)
		if stats != nil {
			if b, err := json.Marshal(stats); err == nil {
				report(string(b))
			}
		}
		return serr
	})
	runner.Register("index_refresh", func(ctx context.Context, payload string, report task.Report) error {
		var p struct {
			LocalPath string `json:"local_path"`
		}
		if payload != "" {
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return fmt.Errorf("index_refresh payload: %w", err)
			}
		}
		stats, err := importer.Import(ctx, p.LocalPath, func(prog string) { report(prog) })
		if stats != nil {
			if b, err := json.Marshal(stats); err == nil {
				report(string(b))
			}
		}
		return err
	})
	go func() {
		if err := runner.Run(ctx); err != nil {
			slog.Error("task runner", "err", err)
		}
	}()

	// 定时扫描（v1 默认 0=仅手动，D-025）。
	if cfg.Scanner.RescanInterval > 0 {
		scanner.StartScheduler(ctx, time.Duration(cfg.Scanner.RescanInterval)*time.Second,
			func(c context.Context) error {
				srcs, err := store.ListSources(c)
				if err != nil {
					return err
				}
				for _, src := range srcs {
					if !src.Enabled {
						continue
					}
					if _, err := runner.Enqueue(c, "scan", scanner.ScanPayload{SourceID: src.ID}.String()); err != nil {
						slog.Error("定时扫描入队失败", "source", src.ID, "err", err)
					}
				}
				return nil
			})
	}

	// 每周自动索引刷新（D-022）：启动时索引缺失/过旧则入队补一次，
	// 之后由每日检查维持。
	bgm := cfg.Metadata.Bangumi
	if bgm.Enabled && bgm.ArchiveIndex.Enabled && bgm.ArchiveIndex.AutoRefresh == "weekly" {
		go autoRefreshIndex(ctx, importer, runner)
	}

	deps := api.Deps{
		DB: database, Sources: store, Scanner: sc, Matcher: mp,
		Review: review.New(database), Tasks: runner, Importer: importer, Index: idx,
	}
	return deps, runner, nil
}

func autoRefreshIndex(ctx context.Context, importer *metadata.Importer, runner *task.Runner) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		st, err := importer.Status(ctx)
		if err == nil && (st.Version == "" || time.Since(st.ImportedTime()) > 7*24*time.Hour) {
			if _, err := runner.Enqueue(ctx, "index_refresh", `{"local_path":""}`); err != nil {
				slog.Error("自动索引刷新入队失败", "err", err)
			} else {
				slog.Info("已入队每周索引刷新")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
