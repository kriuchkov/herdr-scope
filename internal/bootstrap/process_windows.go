//go:build windows

package bootstrap

import (
	"os"
	"os/signal"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
	createNoWindow        = 0x08000000
)

// detached starts the daemon without a console and outside the caller's
// process group, so it outlives the action that started it.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess | createNoWindow, HideWindow: true}
}

// terminateSignal: Windows has no SIGTERM for another process; Kill follows.
func terminateSignal(*os.Process) error { return nil }

func notifyStop(c chan<- os.Signal) {
	signal.Notify(c, os.Interrupt)
}
