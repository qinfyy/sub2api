//go:build windows

package repository

import (
	"syscall"
)

// processAlive 判断 pid 对应的进程是否仍在运行。
//
// Windows 上没有信号 0 探测，标准做法是打开该 PID 对应的进程句柄：
//   - 打开成功 → 进程存在
//   - ERROR_INVALID_PARAMETER → 进程已退出
//   - ERROR_ACCESS_DENIED → 进程存在但属于其他用户或受保护
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const (
		processQueryLimitedInformation = 0x1000
		errorInvalidParameter          = syscall.Errno(87)
		errorAccessDenied              = syscall.Errno(5)
	)

	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		if err == errorInvalidParameter {
			return false
		}
		if err == errorAccessDenied {
			return true
		}
		// 其他错误（如权限不足）不改变“可能存活”的保守判断。
		return true
	}
	defer func() { _ = syscall.CloseHandle(handle) }()
	return true
}
