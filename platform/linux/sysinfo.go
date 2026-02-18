package linux

import (
	"bytes"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// KernelVersion 获取内核版本
func KernelVersion() (uint64, uint64, error) {
	var metadata unix.Utsname
	if err := unix.Uname(&metadata); err != nil {
		return 0, 0, err
	}
	length := bytes.IndexByte(metadata.Release[:], 0)
	if length == -1 {
		return 0, 0, errors.New("invalid system metadata")
	}
	version := string(metadata.Release[:length])
	components := strings.SplitN(version, ".", 3)
	if len(components) != 3 {
		return 0, 0, errors.New("unsupported system version format")
	}
	major, err := strconv.ParseUint(components[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	minor, err := strconv.ParseUint(components[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}

// Capabilities 获取当前进程的权限
func Capabilities() (uint32, error) {
	pid := os.Getpid()
	if pid > math.MaxInt32 {
		return 0, errors.New("process identifier too large")
	}
	header := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_1,
		Pid:     int32(pid),
	}
	var data unix.CapUserData
	if err := unix.Capget(&header, &data); err != nil {
		return 0, err
	}
	return data.Effective, nil
}
