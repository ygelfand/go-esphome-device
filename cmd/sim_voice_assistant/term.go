package main

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// raw terminal mode leaves the cursor mid-line, so output needs a carriage return
// before each newline or the log stair-steps down the screen.
var rawTerminal atomic.Bool

func setRawTerminal(v bool) { rawTerminal.Store(v) }

// status prints an operator-facing line, distinct from the structured log.
func status(msg string) {
	if rawTerminal.Load() {
		fmt.Fprintf(os.Stderr, "\r\n  %s\r\n\r\n", msg)
		return
	}
	fmt.Fprintf(os.Stderr, "\n  %s\n\n", msg)
}

// crlfWriter fixes up newlines while the terminal is in raw mode.
type crlfWriter struct{ w io.Writer }

func (c crlfWriter) Write(p []byte) (int, error) {
	if !rawTerminal.Load() {
		return c.w.Write(p)
	}

	out := make([]byte, 0, len(p)+8)
	for _, b := range p {
		if b == '\n' {
			out = append(out, '\r')
		}
		out = append(out, b)
	}
	if _, err := c.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
