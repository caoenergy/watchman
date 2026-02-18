package cmd

import (
	"fmt"

	"github.com/caoenergy/watchman/internal/loader"
	"github.com/caoenergy/watchman/internal/settings"
	"github.com/caoenergy/watchman/internal/watcher"
	"github.com/caoenergy/watchman/platform/linux"

	"golang.org/x/sys/unix"
)

const (
	// 定义: 内核版本最低版本要求
	MinSupportedKernelMajor = 5
	MinSupportedKernelMinor = 9
	// 定义: 所需的特权
	requiredCaps = uint32((1 << unix.CAP_SYS_ADMIN) | (1 << unix.CAP_DAC_READ_SEARCH))
)

// Initialize 初始化监控引擎;检查内核版本&所需权限和加载设置
func Initialize() (*watcher.Watchman, error) {
	major, minor, err := linux.KernelVersion()
	if err != nil {
		return nil, err
	}
	if major < MinSupportedKernelMajor || (major == MinSupportedKernelMajor && minor < MinSupportedKernelMinor) {
		return nil, fmt.Errorf("expected kernel version >=%d.%d, actual:%d.%d", MinSupportedKernelMajor, MinSupportedKernelMinor, major, minor)
	}
	caps, err := linux.Capabilities()
	if err != nil {
		return nil, err
	}
	if caps&requiredCaps != requiredCaps {
		return nil, fmt.Errorf("insufficient capabilities. try: sudo setcap cap_sys_admin,cap_dac_read_search+ep watchman")
	}
	setting, err := settings.Load()
	if err != nil {
		return nil, err
	}
	wm, err := watcher.Initialize(setting)
	if err != nil {
		return nil, err
	}
	if err := loader.Load(setting.Watchman.PluginRoot, wm); err != nil {
		return nil, err
	}
	return wm, nil
}
