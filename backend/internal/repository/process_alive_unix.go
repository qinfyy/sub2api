//go:build !windows

package repository

import (
	"os"
	"syscall"
)

// processAlive 判断 pid 对应的进程是否仍在运行。
// 使用 signal 0 做存在性探测，不实际发送信号。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal(0) 不投递信号，仅做权限与存在性检查。
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// ESRCH 表示进程不存在；EPERM 表示进程存在但属于其他用户。
	return err == os.ErrPermission || err == syscall.EPERM
}
