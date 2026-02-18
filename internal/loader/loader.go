package loader

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"plugin"

	wmp "github.com/caoenergy/watchman-plugin"
	"github.com/caoenergy/watchman/internal/watcher"
)

func Load(dir string, wm *watcher.Watchman) error {
	if dir == "" {
		return nil
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*.so"))
	if err != nil {
		return fmt.Errorf("plugin dir list: %w", err)
	}
	for _, path := range entries {
		if err := loadOne(path, wm); err != nil {
			slog.Error("load plugin failed", "path", path, "err", err)
			continue
		}
		slog.Info("plugin loaded", "path", path)
	}
	return nil
}

func loadOne(path string, wm *watcher.Watchman) error {
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("plugin open: %w", err)
	}
	sym, err := p.Lookup(wmp.PluginSymbolName)
	if err != nil {
		return fmt.Errorf("lookup %s: %w", wmp.PluginSymbolName, err)
	}
	handler, ok := sym.(*wmp.Handler)
	if !ok {
		return fmt.Errorf("symbol %s is not *plugin.Handler", wmp.PluginSymbolName)
	}
	if *handler == nil {
		return fmt.Errorf("symbol %s is nil", wmp.PluginSymbolName)
	}
	h := *handler
	name := h.Name()
	if name == "" {
		return fmt.Errorf("plugin name is empty")
	}
	if err = h.Init(); err != nil {
		return err
	}
	wm.RegisterPlugin(handler)
	return nil
}
