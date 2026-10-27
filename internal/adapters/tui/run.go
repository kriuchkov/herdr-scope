package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/term"
)

// readBufferSize holds a key press or a modest paste in one read.
const readBufferSize = 256

// Run is the popup on a terminal: raw mode, one frame per key press and per
// resize, until a quit. The terminal is restored however Run ends, a panic
// included.
func Run(ctx context.Context, in, out *os.File, deps Deps) error {
	inFd, outFd := int(in.Fd()), int(out.Fd())
	if !term.IsTerminal(inFd) {
		return errors.New("settings: needs a terminal (Herdr opens it as a popup)")
	}
	popup, err := newEditor(ctx, deps)
	if err != nil {
		return err
	}
	saved, err := term.MakeRaw(inFd)
	if err != nil {
		return fmt.Errorf("settings: raw mode: %w", err)
	}
	defer func() { _ = term.Restore(inFd, saved) }()

	resize := make(chan os.Signal, 1)
	notifyResize(resize)
	defer signal.Stop(resize)

	done := make(chan struct{})
	defer close(done)
	keys := make(chan string)
	failed := make(chan error, 1)
	go readKeys(in, keys, failed, done)

	draw := func() {
		// An unknown size reads as 0 by 0, which Render draws at the default
		// popup size.
		cols, rows, _ := term.GetSize(outFd)
		// Raw mode also turns off output processing, so a bare newline would
		// move down without returning to the first column.
		fmt.Fprint(out, strings.ReplaceAll(popup.Render(cols, rows), "\n", "\r\n"))
	}
	draw()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resize:
			draw()
		case err = <-failed:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("settings: read keys: %w", err)
		case key := <-keys:
			if popup.Key(ctx, key) {
				fmt.Fprint(out, clearScreen)
				return nil
			}
			draw()
		}
	}
}

// readKeys hands each read on as one key: an
// escape sequence arrives whole, and a paste arrives as one string to type.
// A read in progress cannot be interrupted, so the goroutine outlives Run
// until the next byte arrives or the process exits; done only stops it from
// handing that byte on.
func readKeys(in io.Reader, keys chan<- string, failed chan<- error, done <-chan struct{}) {
	buf := make([]byte, readBufferSize)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			select {
			case keys <- string(buf[:n]):
			case <-done:
				return
			}
		}
		if err != nil {
			failed <- err
			return
		}
	}
}
