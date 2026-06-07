package cli

import (
	"io"
	"sync"
	"time"
)

const logTimeLayout = "2006-01-02 15:04:05"

type timestampWriter struct {
	mu          sync.Mutex
	writer      io.Writer
	now         func() time.Time
	atLineStart bool
}

func NewTimestampWriter(writer io.Writer) io.Writer {
	return newTimestampWriter(writer, time.Now)
}

func newTimestampWriter(writer io.Writer, now func() time.Time) io.Writer {
	return &timestampWriter{
		writer:      writer,
		now:         now,
		atLineStart: true,
	}
}

func (w *timestampWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written := 0
	start := 0

	for start < len(p) {
		if w.atLineStart {
			prefix := "[" + w.now().Format(logTimeLayout) + "] "
			if _, err := io.WriteString(w.writer, prefix); err != nil {
				return written, err
			}
			w.atLineStart = false
		}

		newlineIndex := start
		for newlineIndex < len(p) && p[newlineIndex] != '\n' {
			newlineIndex++
		}
		if newlineIndex < len(p) {
			newlineIndex++
		}

		chunk := p[start:newlineIndex]
		n, err := w.writer.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}

		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			w.atLineStart = true
		}
		start = newlineIndex
	}

	return len(p), nil
}
