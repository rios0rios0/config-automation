package repositories

import (
	"bytes"
	"io"
	"sync"
)

// lockedBuffer is a buffer two goroutines may write at once, as a command's
// stdout and stderr copiers do when both feed one combined capture.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

// Write appends data.
func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

// String returns everything written so far.
func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

// lockedWriter serializes the writes two goroutines make to one writer.
type lockedWriter struct {
	mutex  sync.Mutex
	target io.Writer
}

// Write passes data on under the lock.
func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.target.Write(data)
}
