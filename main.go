package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/caoenergy/watchman/cmd"
	"github.com/caoenergy/watchman/internal/listener"
)

func main() {
	wm, err := cmd.Initialize()
	if err != nil {
		slog.Error("failed to initialize app", "err", err)
		os.Exit(-1)
	}
	defer wm.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		slog.Info("received signal, triggering shutdown", "signal", sig)
		cancel()
		wm.Stop() // 关闭 ffd/eventChan，让 captureEvents 和 processEvents 能退出，否则会死锁
	}()
	wm.AddListener("logging", listener.LoggingHandler)
	var wg sync.WaitGroup
	wm.Watch(ctx, &wg)
	wg.Wait()
}
