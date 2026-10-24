package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"fastlink-tool/internal/app"
	"fastlink-tool/internal/platform"
)

var version = "1.1.1-dev"

func main() {
	dev := flag.Bool("dev", false, "development mode: console logs, disk assets, .run-dev state")
	root := flag.String("root", "", "state root (directory containing .run)")
	port := flag.Int("port", 0, "override HTTP port")
	noOpen := flag.Bool("no-open", false, "do not open a browser")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	cwd, _ := os.Getwd()
	webDir := ""
	if *dev {
		webDir = filepath.Join(cwd, "web")
	}
	if !*dev {
		logPath := filepath.Join(firstRoot(*root), ".run", "server.out.log")
		_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			slog.SetDefault(slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo})))
			defer file.Close()
		}
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	err := app.Run(app.Options{
		Dev:     *dev,
		Root:    *root,
		WebDir:  webDir,
		Port:    *port,
		NoOpen:  *noOpen,
		Version: version,
	})
	if err != nil {
		slog.Error("fastlink-tool failed", "error", err)
		if !*dev {
			platform.MessageBox("FastLink-tool", err.Error())
		}
		os.Exit(1)
	}
}

func firstRoot(root string) string {
	if root != "" {
		return root
	}
	return platform.ExecutableDir()
}
