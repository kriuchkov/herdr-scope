//go:build !windows

package bootstrap

import (
	"os"
	"os/signal"
	"syscall"
)

// detached starts the daemon in a session of its own.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// terminateSignal asks the daemon to run its own shutdown.
func terminateSignal(p *os.Process) error {
	return p.Signal(syscall.SIGTERM) //nolint:wrapcheck // best effort
}

// notifyStop delivers the signals that end the daemon politely.
func notifyStop(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
}
