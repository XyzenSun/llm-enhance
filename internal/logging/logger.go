package logging

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Logger interface {
	Debug(string, Fields)
	Info(string, Fields)
	Warn(string, Fields)
	Error(string, Fields)
	Close() error
}
type Fields map[string]any

type noopLogger struct{}

func NewNoop() Logger                   { return noopLogger{} }
func (noopLogger) Debug(string, Fields) {}
func (noopLogger) Info(string, Fields)  {}
func (noopLogger) Warn(string, Fields)  {}
func (noopLogger) Error(string, Fields) {}
func (noopLogger) Close() error         { return nil }

type JSONLogger struct {
	out       io.Writer
	min       int
	closeFunc func() error
	events    chan []byte
	done      chan struct{}
	once      sync.Once
	wg        sync.WaitGroup
}

const defaultAsyncBuffer = 1024

func NewConsole(level string) Logger { return newJSONLogger(os.Stdout, level, nil) }
func NewWriter(out io.Writer, level string) Logger {
	return newJSONLogger(out, level, nil)
}

func newJSONLogger(out io.Writer, level string, closeFunc func() error) *JSONLogger {
	l := &JSONLogger{out: out, min: levelValue(level), closeFunc: closeFunc, events: make(chan []byte, defaultAsyncBuffer), done: make(chan struct{})}
	l.wg.Add(1)
	go l.writeLoop()
	return l
}

func (l *JSONLogger) Debug(msg string, f Fields) { l.log("debug", msg, f) }
func (l *JSONLogger) Info(msg string, f Fields)  { l.log("info", msg, f) }
func (l *JSONLogger) Warn(msg string, f Fields)  { l.log("warn", msg, f) }
func (l *JSONLogger) Error(msg string, f Fields) { l.log("error", msg, f) }
func (l *JSONLogger) Close() error {
	l.once.Do(func() {
		close(l.events)
		l.wg.Wait()
		close(l.done)
	})
	if l.closeFunc != nil {
		return l.closeFunc()
	}
	return nil
}

func (l *JSONLogger) log(level, msg string, f Fields) {
	if levelValue(level) < l.min {
		return
	}
	rec := Fields{"level": level, "time": time.Now().Format(time.RFC3339), "message": sanitizeString(msg)}
	for k, v := range f {
		if isSensitiveKey(k) {
			rec[k+"_present"] = true
			continue
		}
		rec[k] = sanitizeValue(k, v)
	}
	b, _ := json.Marshal(rec)
	line := append(b, '\n')
	select {
	case l.events <- line:
	case <-l.done:
	default:
	}
}

func (l *JSONLogger) writeLoop() {
	defer l.wg.Done()
	for line := range l.events {
		_, _ = l.out.Write(line)
	}
}

func levelValue(l string) int {
	switch l {
	case "debug":
		return 0
	case "info", "":
		return 1
	case "warn":
		return 2
	case "error":
		return 3
	default:
		return 1
	}
}
func isSensitiveKey(k string) bool {
	s := strings.ToLower(k)
	return strings.Contains(s, "authorization") || strings.Contains(s, "api_key") || strings.Contains(s, "x-api-key") || strings.Contains(s, "x-goog-api-key") || strings.Contains(s, "token") || strings.Contains(s, "secret") || strings.Contains(s, "key") || strings.Contains(s, "body") || strings.Contains(s, "prompt") || strings.Contains(s, "message_content") || strings.Contains(s, "custom_value")
}
func sanitizeValue(k string, v any) any {
	if isSensitiveKey(k) {
		return true
	}
	if s, ok := v.(string); ok {
		return sanitizeString(s)
	}
	return v
}
func sanitizeString(s string) string { return s }
