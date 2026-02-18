package watcher

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	wmp "github.com/caoenergy/watchman-plugin"
	"github.com/caoenergy/watchman/internal/settings"

	"github.com/armon/go-radix"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sys/unix"
)

type Watchman struct {
	ffd             int // fanotifyFd
	rfd             int // rootFd
	fdcManager      *lru.LRU[string, string]
	fpcManager      *lru.LRU[string, string]
	filter          *radix.Tree
	filterMu        sync.RWMutex
	eventChan       chan Event
	eventBufferSize int
	listeners       map[string]Listener
	listenerMu      sync.RWMutex
	stopOnce        sync.Once
	plugins         []*wmp.Handler
}

type Event struct {
	Mask   uint64
	IsDir  bool
	Handle []byte
}

// Listener 接收事件回调。实现方应尽快返回，避免阻塞事件处理；若有耗时 I/O 请自行起 goroutine 或投递到自有队列。
type Listener func(eventType, dir, filename string, isDir bool)

const (
	EventMetadataLen = int(unsafe.Sizeof(unix.FanotifyEventMetadata{}))
	// struct fanotify_event_info_header + fsid
	// info_type(1) + pad(1) + len(2) + fsid(8) = 12
	EventInfoFidLen = 12
	// struct file_handle 头部：handle_bytes(4) + handle_type(4)
	FileHandleLen = 8
)

func Initialize(setting *settings.Settings) (*Watchman, error) {
	// FAN_REPORT_DFID_NAME requires Linux kernel 5.9 or higher.
	ffd, err := unix.FanotifyInit(unix.FAN_REPORT_DFID_NAME|unix.FAN_CLOEXEC, unix.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}

	if err = unix.FanotifyMark(ffd, unix.FAN_MARK_ADD|unix.FAN_MARK_FILESYSTEM,
		unix.FAN_CREATE|unix.FAN_DELETE|unix.FAN_DELETE_SELF|unix.FAN_CLOSE_WRITE|unix.FAN_MOVED_TO|unix.FAN_ONDIR|unix.FAN_EVENT_ON_CHILD,
		unix.AT_FDCWD, "/"); err != nil {
		_ = unix.Close(ffd)
		return nil, fmt.Errorf("mark: %w", err)
	}

	rfd, err := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(ffd)
		return nil, fmt.Errorf("open root: %w", err)
	}
	filter := radix.New()
	for _, p := range setting.Watchman.Watcher.Paths {
		filter.Insert(p, true)
		slog.Info("添加监控路径", "path", p)
	}
	eventBufferSize := setting.Watchman.Watcher.BufferSize
	if eventBufferSize <= 0 {
		eventBufferSize = 64
	}
	return &Watchman{
		ffd:             ffd,
		rfd:             rfd,
		fdcManager:      lru.NewLRU[string, string](setting.Watchman.Cache.FdSize, nil, time.Duration(setting.Watchman.Cache.FdTtl)*time.Second),
		fpcManager:      lru.NewLRU[string, string](setting.Watchman.Cache.FpSize, nil, time.Duration(setting.Watchman.Cache.FpTtl)*time.Second),
		filter:          filter,
		eventChan:       make(chan Event, 4096),
		eventBufferSize: eventBufferSize,
		listeners:       make(map[string]Listener),
		plugins:         make([]*wmp.Handler, 0),
	}, nil
}

func (wm *Watchman) Stop() {
	wm.stopOnce.Do(func() {
		if wm != nil {
			// 先关 ffd，使 captureEvents 的 Read 返回并退出；再关 channel 让 processEvents 退出；最后关 rfd
			_ = unix.Close(wm.ffd)
			wm.ffd = -1
			close(wm.eventChan)
			_ = unix.Close(wm.rfd)
			wm.rfd = -1
		}
		if wm.plugins != nil {
			for _, p := range wm.plugins {
				_ = (*p).Close()
			}
		}
	})
}

func (wm *Watchman) RegisterPlugin(p *wmp.Handler) {
	wm.plugins = append(wm.plugins, p)
	wm.AddListener((*p).Name(), (*p).Handle)
}

func (wm *Watchman) AddListener(identify string, listener Listener) {
	wm.listenerMu.Lock()
	defer wm.listenerMu.Unlock()
	wm.listeners[identify] = listener
}

func (wm *Watchman) RemoveListener(identify string) {
	wm.listenerMu.Lock()
	defer wm.listenerMu.Unlock()
	delete(wm.listeners, identify)
}

func (wm *Watchman) Watch(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		wm.captureEvents(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		wm.processEvents(ctx)
	}()
}

func (wm *Watchman) captureEvents(ctx context.Context) {
	buffer := make([]byte, wm.eventBufferSize*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// 读取事件数据，可能读取到多个事件
			read, err := unix.Read(wm.ffd, buffer)
			if err != nil {
				if errors.Is(err, unix.EBADF) || errors.Is(err, unix.EINTR) {
					return
				}
				continue
			}

			data := buffer[:read]
			// 循环处理每个事件
			for len(data) >= EventMetadataLen {
				// 事件长度
				eventLen := binary.LittleEndian.Uint32(data[0:4])
				if int(eventLen) > len(data) || eventLen == 0 {
					break
				}
				// 检查事件版本, 只处理版本为3的事件
				if data[4] != unix.FANOTIFY_METADATA_VERSION {
					data = data[eventLen:]
					continue
				}
				// 读取事件掩码
				mask := binary.LittleEndian.Uint64(data[8:16])
				// 检查溢出标志
				if mask&unix.FAN_Q_OVERFLOW != 0 {
					slog.Warn("queue overflow - events lost")
					data = data[eventLen:]
					continue
				}
				// 读取事件数据
				eventData := data[EventMetadataLen:eventLen]

				var handle []byte
				if len(eventData) >= EventInfoFidLen+FileHandleLen {
					handle = make([]byte, len(eventData))
					copy(handle, eventData)
				}

				select {
				case <-ctx.Done():
					return
				case wm.eventChan <- Event{
					Mask:   mask,
					IsDir:  (mask & unix.FAN_ONDIR) != 0,
					Handle: handle,
				}:
				}
				// 移动到下一个事件
				data = data[eventLen:]
			}
		}
	}
}

func (wm *Watchman) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-wm.eventChan:
			if !ok {
				return
			}
			directory, filename, ok := wm.resolve(event.Handle)
			if !ok || (directory == "" || filename == "") {
				continue
			}
			fullPath := filepath.Join(directory, filename)
			if event.IsDir {
				continue
			}

			wm.filterMu.RLock()
			_, _, matched := wm.filter.LongestPrefix(fullPath)
			wm.filterMu.RUnlock() // 尽快释放锁，不要用 defer 因为会拉长锁时间
			if !matched {
				continue
			}
			eventType := wm.maskToString(event.Mask)
			if _, ok = wm.fpcManager.Get(fullPath); ok {
				continue
			}
			wm.fpcManager.Add(fullPath, eventType)

			wm.listenerMu.RLock()
			snapshot := make(map[string]Listener, len(wm.listeners))
			for k, v := range wm.listeners {
				snapshot[k] = v
			}
			wm.listenerMu.RUnlock()
			for _, l := range snapshot {
				l(eventType, directory, filename, event.IsDir)
			}
		}
	}
}

func (wm *Watchman) resolve(data []byte) (string, string, bool) {
	if len(data) < EventInfoFidLen {
		return "", "", false
	}

	infoType := data[0]
	handleData := data[EventInfoFidLen:]
	if len(handleData) < FileHandleLen {
		return "", "", false
	}

	handleBytes := binary.LittleEndian.Uint32(handleData[0:4])
	handleType := int32(binary.LittleEndian.Uint32(handleData[4:8]))

	if int(handleBytes) > len(handleData)-FileHandleLen {
		return "", "", false
	}

	handleRaw := handleData[FileHandleLen : FileHandleLen+int(handleBytes)]
	cacheKey := base64.StdEncoding.EncodeToString(handleRaw)

	basePath, ok := wm.fdcManager.Get(cacheKey)

	if !ok {
		fh := unix.NewFileHandle(handleType, handleRaw)
		fd, err := unix.OpenByHandleAt(wm.rfd, fh, unix.O_PATH|unix.O_CLOEXEC)
		if err != nil {
			return "", "", false
		}
		basePath, err = os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		_ = unix.Close(fd)
		if err != nil {
			return "", "", false
		}
		wm.fdcManager.Add(cacheKey, basePath)
	}

	if infoType == unix.FAN_EVENT_INFO_TYPE_DFID_NAME {
		nameOffset := FileHandleLen + int(handleBytes)
		if len(handleData) > nameOffset {
			rest := handleData[nameOffset:]
			if i := bytes.IndexByte(rest, 0); i >= 0 {
				rest = rest[:i]
			}
			name := string(rest)
			if name != "" && basePath != "" {
				if basePath == "/" {
					return "/", name, true
				}
				return basePath, name, true
			}
		}
	}

	return basePath, "", true
}

func (wm *Watchman) maskToString(mask uint64) string {
	var events []string
	if mask&unix.FAN_CREATE != 0 {
		events = append(events, "CREATE")
	}
	if mask&unix.FAN_DELETE != 0 {
		events = append(events, "DELETE")
	}
	if mask&unix.FAN_DELETE_SELF != 0 {
		events = append(events, "DELETE_SELF")
	}
	if mask&unix.FAN_CLOSE_WRITE != 0 {
		events = append(events, "CLOSE_WRITE")
	}
	if mask&unix.FAN_MOVED_TO != 0 {
		events = append(events, "MOVED_TO")
	}
	if len(events) == 0 {
		return fmt.Sprintf("0x%x", mask)
	}
	return strings.Join(events, "|")
}
