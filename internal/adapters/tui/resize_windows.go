//go:build windows

package tui

import "os"

// notifyResize does nothing: a Windows console sends no resize signal, so
// the popup picks up a new size on the next key press.
func notifyResize(chan<- os.Signal) {}
