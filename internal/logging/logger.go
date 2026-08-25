package logging

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return DebugLevel, nil
	case "info", "":
		return InfoLevel, nil
	case "warn", "warning":
		return WarnLevel, nil
	case "error":
		return ErrorLevel, nil
	}
	return InfoLevel, &levelError{s}
}

type levelError struct{ got string }

func (e *levelError) Error() string { return "unknown log level: " + e.got }

// Logger emits structured JSON lines: {"ts","level","msg","kv..."}.
type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	base  map[string]any
	level Level
}

func New(component string, level Level) *Logger {
	return &Logger{
		out:   os.Stderr,
		base:  map[string]any{"component": component},
		level: level,
	}
}

func (l *Logger) With(kv ...any) *Logger {
	n := &Logger{out: l.out, level: l.level, base: map[string]any{}}
	for k, v := range l.base {
		n.base[k] = v
	}
	applyKV(n.base, kv)
	return n
}

func (l *Logger) Debug(msg string, kv ...any) { l.log(DebugLevel, msg, kv...) }
func (l *Logger) Info(msg string, kv ...any)  { l.log(InfoLevel, msg, kv...) }
func (l *Logger) Warn(msg string, kv ...any)  { l.log(WarnLevel, msg, kv...) }
func (l *Logger) Error(msg string, kv ...any) { l.log(ErrorLevel, msg, kv...) }

func (l *Logger) log(level Level, msg string, kv ...any) {
	if level < l.level {
		return
	}
	rec := map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano), "level": level.String(), "msg": msg}
	for k, v := range l.base {
		rec[k] = v
	}
	applyKV(rec, kv)

	b, err := json.Marshal(rec)
	if err != nil {
		b, _ = json.Marshal(map[string]any{"ts": time.Now().UTC(), "level": "error", "msg": "log-marshal-failed", "orig": msg})
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out.Write(append(b, '\n'))
}

func applyKV(m map[string]any, kv []any) {
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			v := kv[i+1]
			if err, isErr := v.(error); isErr {
				v = err.Error()
			}
			m[k] = v
		}
	}
}

func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "debug"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	default:
		return "info"
	}
}
