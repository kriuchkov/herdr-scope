//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyResize delivers SIGWINCH, the terminal's word for a new size.
func notifyResize(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGWINCH)
}
