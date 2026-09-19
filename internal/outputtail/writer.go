// Package outputtail provides fixed-size command-output capture for diagnostics.
package outputtail

// Writer implements io.Writer while retaining only the last limit bytes written.
// It returns len(p) from Write even when older bytes are discarded, matching the
// io.Writer contract for successful full consumption.
type Writer struct {
	limit int
	buf   []byte
}

// New returns a Writer that keeps at most limit bytes. Non-positive limits
// discard all input while still reporting successful writes.
func New(limit int) *Writer {
	if limit < 0 {
		limit = 0
	}
	return &Writer{limit: limit, buf: make([]byte, 0, limit)}
}

// Write records p's trailing bytes, discarding older content beyond the limit.
func (w *Writer) Write(p []byte) (int, error) {
	n := len(p)
	if w.limit == 0 || n == 0 {
		return n, nil
	}
	if n >= w.limit {
		w.buf = append(w.buf[:0], p[n-w.limit:]...)
		return n, nil
	}
	if overflow := len(w.buf) + n - w.limit; overflow > 0 {
		copy(w.buf, w.buf[overflow:])
		w.buf = w.buf[:len(w.buf)-overflow]
	}
	w.buf = append(w.buf, p...)
	return n, nil
}

// String returns the retained tail as a string.
func (w *Writer) String() string {
	return string(w.buf)
}
