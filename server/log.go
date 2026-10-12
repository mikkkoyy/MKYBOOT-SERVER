package main

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sync"
	"time"
)

// Logger emits structured JSON-line logs and is safe for concurrent use.
// It never logs secrets: callers must pass only non-sensitive fields.
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer
}

// NewLogger returns a logger writing to w.
func NewLogger(w io.Writer) *Logger { return &Logger{w: w} }

// NewFileLogger opens (creating parent directories) an append-only log file
// with 0600 permissions and returns a logger bound to it.
func NewFileLogger(path string) (*Logger, error) {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	return &Logger{w: f, closer: f}, nil
}

func (l *Logger) log(level, msg string, fields map[string]any) {
	if l == nil {
		return
	}
	entry := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"level": level,
		"msg":   msg,
	}
	for k, v := range fields {
		entry[k] = v
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = l.w.Write(append(b, '\n'))
}

// Info logs at info level.
func (l *Logger) Info(msg string, fields map[string]any) { l.log("info", msg, fields) }

// Warn logs at warn level.
func (l *Logger) Warn(msg string, fields map[string]any) { l.log("warn", msg, fields) }

// Error logs at error level.
func (l *Logger) Error(msg string, fields map[string]any) { l.log("error", msg, fields) }

// Close releases the underlying file, if any.
func (l *Logger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}
