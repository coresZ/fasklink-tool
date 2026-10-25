package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"fastlink-tool/internal/config"
	"fastlink-tool/internal/httpapi"
	"fastlink-tool/internal/platform"
	"fastlink-tool/internal/service"
	"fastlink-tool/internal/store"
	"fastlink-tool/web"
)

type Options struct {
	Dev     bool
	Root    string
	WebDir  string
	Port    int
	NoOpen  bool
	Version string
}

func Run(opts Options) error {
	root := opts.Root
	if root == "" {
		if opts.Dev {
			cwd, _ := os.Getwd()
			root = filepath.Join(cwd, ".run-dev")
		} else {
			root = platform.ExecutableDir()
		}
	}
	root, _ = filepath.Abs(root)

	if opts.Dev {
		if _, err := os.Stat(config.Path(root)); errors.Is(err, os.ErrNotExist) {
			// Seed from config.Default() so dev and production start from the same
			// listener scope. Hardcoding 127.0.0.1 here made every fresh dev state
			// loopback-only, so phones on the LAN could not open the panel or its
			// subscription links until the setting was changed by hand.
			seed := config.Default()
			seed.LogLevel = "debug"
			_ = config.Save(root, seed)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if opts.Port > 0 {
		cfg.Port = opts.Port
	}
	// Dev mode used to force cfg.Host = "127.0.0.1", which silently overrode the
	// 监听范围 setting on every restart: saving "局域网 + 本机" appeared to work but
	// the listener still bound loopback only. Honour the config; dev only defaults
	// to loopback when no config file exists yet.
	if config.SeedDomains(&cfg) {
		_ = config.Save(root, cfg)
	}

	var releaseMutex func()
	if !opts.Dev {
		release, already, err := platform.AcquireMutex(`Local\FastLinkTool`)
		if err != nil {
			slog.Warn("single instance mutex", "error", err)
		} else if already {
			if !opts.NoOpen {
				_ = platform.OpenBrowser(panelURL(cfg))
			}
			return nil
		} else {
			releaseMutex = release
			defer releaseMutex()
		}
	}

	st, err := store.Open(root)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.EnsureAllSubKeys(); err != nil {
		return err
	}

	svc := service.New(root, cfg, st)
	svc.PrimeFromCache()

	webFS := webFS(opts)
	quit := make(chan struct{})
	var once sync.Once
	closeQuit := func() {
		once.Do(func() { close(quit) })
	}

	api := httpapi.New(svc, cfg, root, webFS, opts.Version, closeQuit)
	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port)),
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "error", err)
		}
	}()
	slog.Info("listening", "addr", server.Addr, "version", opts.Version)

	if !opts.NoOpen {
		_ = platform.OpenBrowser(panelURL(cfg))
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	select {
	case <-quit:
	case <-signalCh:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	return nil
}

func webFS(opts Options) fs.FS {
	if opts.Dev && opts.WebDir != "" {
		return os.DirFS(opts.WebDir)
	}
	return web.FS
}

func panelURL(cfg config.Config) string {
	if cfg.Host == "0.0.0.0" {
		if lan := config.LANAddresses(); len(lan) > 0 {
			return "http://" + lan[0] + ":" + fmt.Sprint(cfg.Port) + "/"
		}
	}
	return "http://127.0.0.1:" + fmt.Sprint(cfg.Port) + "/"
}
