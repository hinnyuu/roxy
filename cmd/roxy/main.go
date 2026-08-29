package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hinnyuu/roxy/internal/api"
	"github.com/hinnyuu/roxy/internal/auth"
	"github.com/hinnyuu/roxy/internal/config"
	"github.com/hinnyuu/roxy/internal/db"
)

var version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println("roxy", version)
			return 0
		case "help", "--help", "-h":
			printUsage()
			return 0
		case "serve":
			args = args[1:]
		default:
			fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", args[0])
			printUsage()
			return 2
		}
	}

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "配置文件路径（可不存在，使用默认值与环境变量）")
	showVersion := fs.Bool("version", false, "打印版本号")
	fs.Parse(args)
	if *showVersion {
		fmt.Println("roxy", version)
		return 0
	}

	if err := serve(*configPath); err != nil {
		slog.Error("roxy exited", "err", err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Print(`roxy — LLM-agent-driven anime media library organizer

用法:
  roxy [serve] [--config PATH]   启动服务（默认命令）
  roxy version                   打印版本号
  roxy help                      显示本帮助

配置优先级: 默认值 < 配置文件 (YAML) < ROXY_* 环境变量。
详见 docs/ARCHITECTURE.md §13。
`)
}

func serve(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.DataDir, err)
	}
	database, err := db.Open(filepath.Join(cfg.DataDir, "roxy.db"))
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(ctx, database); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	authSvc := auth.NewService(database)
	if err := authSvc.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap auth: %w", err)
	}
	sessions := auth.NewSessionStore()

	srv := &http.Server{
		Addr:    net.JoinHostPort(cfg.Server.Host, fmt.Sprint(cfg.Server.Port)),
		Handler: api.NewServer(cfg, authSvc, sessions, version).Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("roxy serving", "addr", srv.Addr, "version", version,
			"data_dir", cfg.DataDir)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}
