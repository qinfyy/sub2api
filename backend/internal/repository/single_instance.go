package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ErrSingleInstanceViolation reports incompatible application instances.
var ErrSingleInstanceViolation = errors.New(
	"selected backend supports a single process only; another instance holds the deployment lock")

// singleInstanceGuard 持有一个进程打开的锁文件。
type singleInstanceGuard struct {
	lockPath string
	file     *os.File
}

var (
	singleInstanceMu    sync.Mutex
	activeInstanceGuard *singleInstanceGuard
)

// ValidateSingleInstanceGuard acquires the file guard for either SQLite mode.
//
// 该组合没有跨进程协调能力（内存缓存不共享），
// 因此必须拒绝同一库文件上的第二个实例，否则会重复执行后台任务、
// 重复计费，并把内存状态与数据库状态撕裂开。
//
// 其他组合在此不做校验；这不代表内存缓存可跨进程共享。
//
// 返回的 guard 必须由调用方在进程退出时交给 ReleaseSingleInstanceGuard 释放；
// nil 表示该组合无需互斥。
func ValidateSingleInstanceGuard(cfg *config.Config) (*singleInstanceGuard, error) {
	if cfg == nil {
		return nil, nil
	}

	dialect := dialectOf(cfg)
	if dialect != config.DatabaseTypeSQLite {
		return nil, nil
	}

	lockPath, err := singleInstanceLockPath(cfg)
	if err != nil {
		return nil, err
	}

	singleInstanceMu.Lock()
	defer singleInstanceMu.Unlock()

	if activeInstanceGuard != nil {
		if activeInstanceGuard.lockPath == lockPath {
			// 同一进程重复校验（例如测试或多处调用）：视为同一个实例。
			return nil, nil
		}
		return nil, fmt.Errorf("%w: this process already guards %s", ErrSingleInstanceViolation, activeInstanceGuard.lockPath)
	}

	guard, err := acquireSingleInstanceLock(lockPath)
	if err != nil {
		return nil, err
	}
	activeInstanceGuard = guard
	return guard, nil
}

// ReleaseSingleInstanceGuard 释放 ValidateSingleInstanceGuard 取得的锁文件。
// guard 为 nil 时是空操作，可无条件调用。
func ReleaseSingleInstanceGuard(guard *singleInstanceGuard) error {
	if guard == nil {
		return nil
	}

	singleInstanceMu.Lock()
	defer singleInstanceMu.Unlock()

	var closeErr error
	if guard.file != nil {
		closeErr = guard.file.Close()
		guard.file = nil
	}
	// Keep the inode: unlinking could let another process lock a different file
	// at the same path while an existing contender still holds the old one.
	if activeInstanceGuard == guard {
		activeInstanceGuard = nil
	}
	return closeErr
}

// singleInstanceLockPath 计算 SQLite 库的锁文件路径。
// 与库文件同目录同名前缀，便于运维识别与清理。
func singleInstanceLockPath(cfg *config.Config) (string, error) {
	path := strings.TrimSpace(cfg.Database.Path)
	if path == "" {
		path = "data/sub2api.db"
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve sqlite database path %s: %w", path, err)
	}

	if dir := filepath.Dir(abs); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create sqlite database directory %s: %w", dir, err)
		}
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else if errors.Is(err, os.ErrNotExist) {
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			return "", err
		}
		abs = filepath.Join(parent, filepath.Base(abs))
	} else {
		return "", err
	}
	return abs + ".lock", nil
}

// The OS releases the lock on process exit, including crashes. PID is metadata,
// never the source of ownership, so PID reuse and namespaces do not affect it.
func acquireSingleInstanceLock(lockPath string) (*singleInstanceGuard, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open single instance lock file: %w", err)
	}
	locked, err := tryLockInstanceFile(file)
	if err != nil || !locked {
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("lock single instance file: %w", err)
		}
		return nil, fmt.Errorf("%w: %s", ErrSingleInstanceViolation, lockPath)
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("truncate single instance lock metadata: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write single instance lock metadata: %w", err)
	}
	return &singleInstanceGuard{lockPath: lockPath, file: file}, nil
}
