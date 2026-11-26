package debuglog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func decode(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line is not JSON: %q: %s", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestEventsAreOneJSONObjectPerLineInOrder(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Event("key", "key", "tab", "focus", 0)
	l.Event("fetch.try", "url", "http://x/", "status", 200, "ok", true, "ms", 1.5)
	evs := decode(t, buf.Bytes())
	if len(evs) != 2 {
		t.Fatalf("events %v", evs)
	}
	if evs[0]["event"] != "key" || evs[0]["key"] != "tab" || evs[1]["status"] != float64(200) || evs[1]["ok"] != true {
		t.Errorf("fields lost: %v", evs)
	}
	if evs[0]["seq"] != float64(1) || evs[1]["seq"] != float64(2) {
		t.Errorf("seq: %v %v", evs[0]["seq"], evs[1]["seq"])
	}
	for _, e := range evs {
		if _, err := time.Parse(time.RFC3339Nano, e["t"].(string)); err != nil {
			t.Errorf("t is not a timestamp: %v", e["t"])
		}
		if _, ok := e["at_ms"].(float64); !ok {
			t.Errorf("no elapsed at_ms: %v", e)
		}
	}
}

// "ms" is the duration of a step; the elapsed time since the start is at_ms, so
// no event carries the same key twice.
func TestMsIsAStepDurationAndNeverDuplicated(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Event("fetch.try", "ms", 12*time.Millisecond)
	line := buf.String()
	if strings.Count(line, `"ms":`) != 1 || !strings.Contains(line, `"ms":12`) {
		t.Errorf("line %q", line)
	}
}

func TestFieldOrderIsReadable(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Event("open", "url", "u", "links", 3)
	line := buf.String()
	if !strings.HasPrefix(line, `{"seq":1,"t":"`) || !strings.Contains(line, `"event":"open","url":"u","links":3}`) {
		t.Errorf("line %q", line)
	}
}

func TestValueTypes(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Event("x", "err", errors.New("boom"), "d", 1500*time.Millisecond, "list", []string{"a", "b"}, "nilerr", error(nil), "quote", "a\"b\n")
	e := decode(t, buf.Bytes())[0]
	if e["err"] != "boom" || e["d"] != float64(1500) || e["nilerr"] != nil || e["quote"] != "a\"b\n" {
		t.Errorf("values: %v", e)
	}
	if l, ok := e["list"].([]any); !ok || len(l) != 2 {
		t.Errorf("list: %v", e["list"])
	}
}

func TestOddKeyValueListIsNotAPanic(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Event("x", "lonely")
	if e := decode(t, buf.Bytes())[0]; e["event"] != "x" {
		t.Errorf("%v", e)
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	l.Event("x", "a", 1) // must not panic
	if FromContext(context.Background()) != nil {
		t.Error("a context with no logger gave one")
	}
	FromContext(context.Background()).Event("x") // and that is safe too
}

func TestContextCarriesTheLogger(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	ctx := WithContext(context.Background(), l)
	FromContext(ctx).Event("y")
	if len(decode(t, buf.Bytes())) != 1 {
		t.Error("event not logged through the context")
	}
}

func TestConcurrentEventsDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Event("e", "n", j)
			}
		}()
	}
	wg.Wait()
	if evs := decode(t, buf.Bytes()); len(evs) != 1000 {
		t.Errorf("%d events, want 1000", len(evs))
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// A debug log must never be the reason the browser stops.
func TestAWriteFailureIsIgnored(t *testing.T) {
	New(failWriter{}).Event("x", "a", 1)
}
