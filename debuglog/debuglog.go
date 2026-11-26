// Package debuglog writes a cmarkbrowser session as JSON lines, so a person
// and a tool can see what was pressed, what was fetched, which fallback
// produced a page and how long each step took. It holds addresses, key names,
// counts and times, never page content. A nil *Logger does nothing, so code
// logs unconditionally and the flag decides whether anything is written.
package debuglog

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

/** Logger writes events, one JSON object per line. Create it with New; the
 * zero value and nil do nothing. It is safe for use from several goroutines.
 *
 * Example:
 *   var l *debuglog.Logger // nil: every call is a no-op
 *   l = debuglog.New(os.Stderr)
 */
type Logger struct {
	mu    sync.Mutex
	w     io.Writer
	start time.Time
	seq   int
}

/** New returns a Logger that writes to w. A write error is dropped: a debug
 * log must never stop the browser.
 *
 * Parameters:
 *   w (io.Writer) — where the lines go, usually a file
 *
 * Returns:
 *   *Logger — ready to use
 *
 * Example:
 *   l := debuglog.New(f)
 */
func New(w io.Writer) *Logger { return &Logger{w: w, start: time.Now()} }

/** Event writes one line: a sequence number from 1, the wall-clock time, at_ms (the
 * milliseconds since the logger was made), the event name, then the fields in
 * the order given. A time.Duration is written as milliseconds, an error as its
 * message, and a nil error as null. An odd trailing key is ignored.
 *
 * Parameters:
 *   name (string) — the event, such as "key", "fetch.try", "open"
 *   kv (...any) — alternating field names (strings) and values
 *
 * Example:
 *   l.Event("fetch.try", "role", "same", "url", u, "status", 200, "ms", 12.5)
 */
func (l *Logger) Event(name string, kv ...any) {
	if l == nil || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	now := time.Now()
	var b bytes.Buffer
	b.WriteString(`{"seq":`)
	b.Write(enc(l.seq))
	b.WriteString(`,"t":`)
	b.Write(enc(now.Format(time.RFC3339Nano)))
	b.WriteString(`,"at_ms":`)
	b.Write(enc(float64(now.Sub(l.start).Microseconds()) / 1000))
	b.WriteString(`,"event":`)
	b.Write(enc(name))
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			continue
		}
		b.WriteByte(',')
		b.Write(enc(key))
		b.WriteByte(':')
		b.Write(enc(kv[i+1]))
	}
	b.WriteString("}\n")
	_, _ = l.w.Write(b.Bytes())
}

// enc encodes one value as JSON, without escaping & < > (these are addresses),
// turning durations into milliseconds and errors into their message.
func enc(v any) []byte {
	switch x := v.(type) {
	case time.Duration:
		v = float64(x.Microseconds()) / 1000
	case error:
		if x == nil {
			v = nil
		} else {
			v = x.Error()
		}
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return []byte(`null`)
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

type ctxKey struct{}

/** WithContext returns ctx carrying l, so code that already takes a context
 * (page.Fetch, browse.Open) can log without a new parameter.
 *
 * Example:
 *   ctx := debuglog.WithContext(context.Background(), l)
 */
func WithContext(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

/** FromContext returns the Logger in ctx, or nil, which logs nothing.
 *
 * Example:
 *   debuglog.FromContext(ctx).Event("open", "url", u)
 */
func FromContext(ctx context.Context) *Logger {
	l, _ := ctx.Value(ctxKey{}).(*Logger)
	return l
}
