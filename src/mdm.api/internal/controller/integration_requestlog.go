package controller

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxLoggedBody caps how much of a request or response body is kept per call.
const maxLoggedBody = 8 << 10

// RequestRecord is one call to the integration API, as kept for the settings
// screen. The server log gets a one-line summary of it; the bodies are kept only
// here, in memory, for the system administrator. The API key (a header) is never
// recorded — only the start of an unrecognised one, to tell which key was used.
type RequestRecord struct {
	ID         uint64    `json:"id"`
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Query      string    `json:"query"`
	Status     int       `json:"status"`
	DurationMs int64     `json:"duration_ms"`
	Client     string    `json:"client"` // "" when the call did not get past authentication
	IP         string    `json:"ip"`     // the caller's address (CF-Connecting-IP behind the tunnel)
	IdemKey    string    `json:"idempotency_key"`
	ErrorCode  string    `json:"error_code"`    // the API error code, if the call failed
	ErrorMsg   string    `json:"error_message"` // and its message
	KeyHint    string    `json:"key_hint"`      // first characters of an unrecognised key
	Replayed   bool      `json:"replayed"`      // answered from a stored Idempotency-Key result

	// Only in the detail view (Get), not in the list.
	RequestBody   string `json:"request_body,omitempty"`
	ResponseBody  string `json:"response_body,omitempty"`
	ReqTruncated  bool   `json:"request_truncated,omitempty"`
	RespTruncated bool   `json:"response_truncated,omitempty"`
}

// RequestLog keeps the most recent calls in memory (it starts empty after a
// restart; the server log keeps the one-line history).
type RequestLog struct {
	mu   sync.Mutex
	buf  []RequestRecord
	next int
	full bool
	seq  uint64
}

func NewRequestLog(size int) *RequestLog {
	if size < 1 {
		size = 1
	}
	return &RequestLog{buf: make([]RequestRecord, size)}
}

// Add stores a record and gives it its id.
func (l *RequestLog) Add(r RequestRecord) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	r.ID = l.seq
	l.buf[l.next] = r
	l.next = (l.next + 1) % len(l.buf)
	if l.next == 0 {
		l.full = true
	}
}

func (l *RequestLog) count() int {
	if l.full {
		return len(l.buf)
	}
	return l.next
}

// Recent returns up to limit records, newest first, without their bodies.
func (l *RequestLog) Recent(limit int) []RequestRecord {
	if l == nil {
		return []RequestRecord{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.count()
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]RequestRecord, 0, limit)
	for i := 1; i <= limit; i++ {
		r := l.buf[(l.next-i+len(l.buf))%len(l.buf)]
		r.RequestBody, r.ResponseBody, r.ReqTruncated, r.RespTruncated = "", "", false, false
		out = append(out, r)
	}
	return out
}

// Get returns one record with its bodies; false once it has been pushed out.
func (l *RequestLog) Get(id uint64) (RequestRecord, bool) {
	if l == nil {
		return RequestRecord{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := 0; i < l.count(); i++ {
		if l.buf[i].ID == id {
			return l.buf[i], true
		}
	}
	return RequestRecord{}, false
}

// capture keeps the first maxLoggedBody bytes that pass through it.
type capture struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *capture) add(p []byte) {
	room := maxLoggedBody - c.buf.Len()
	if room <= 0 {
		if len(p) > 0 {
			c.truncated = true
		}
		return
	}
	if len(p) > room {
		c.buf.Write(p[:room])
		c.truncated = true
		return
	}
	c.buf.Write(p)
}

// teeBody records what the handler reads of the request body.
type teeBody struct {
	io.ReadCloser
	c *capture
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	t.c.add(p[:n])
	return n, err
}

// loggingWriter records the status and body of a response and what the handlers
// note about the call (who, from where, which error).
type loggingWriter struct {
	http.ResponseWriter
	status int
	rec    *RequestRecord
	resp   capture
}

func (w *loggingWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.resp.add(b)
	return w.ResponseWriter.Write(b)
}

// noter is implemented by the writer the logging middleware hands to handlers.
type noter interface {
	noteCaller(client, ip, keyHint string)
	noteError(code, message string)
}

func (w *loggingWriter) noteCaller(client, ip, keyHint string) {
	w.rec.Client, w.rec.IP, w.rec.KeyHint = client, ip, keyHint
}
func (w *loggingWriter) noteError(code, message string) {
	w.rec.ErrorCode, w.rec.ErrorMsg = code, message
}

func noteCaller(w http.ResponseWriter, client, ip, keyHint string) {
	if n, ok := w.(noter); ok {
		n.noteCaller(client, ip, keyHint)
	}
}

// keyHint is the start of a presented key, enough to tell a stale key from a
// current one (the settings screen shows the current key's prefix) without
// recording anything usable.
func keyHint(r *http.Request) string {
	_, key, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return ""
	}
	if len(key) > 10 {
		key = key[:10]
	}
	return key
}

// logged records every call to the integration API (except the health probe,
// which would drown the rest): one line in the server log, and the full record
// in the in-memory list shown on the settings screen.
func (c *IntegrationController) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &RequestRecord{
			Time: start, Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			IdemKey: r.Header.Get("Idempotency-Key"),
		}
		var req capture
		if r.Body != nil {
			r.Body = &teeBody{ReadCloser: r.Body, c: &req}
		}
		lw := &loggingWriter{ResponseWriter: w, rec: rec}
		defer func() {
			rec.Status = lw.status
			if rec.Status == 0 {
				rec.Status = http.StatusOK
			}
			rec.DurationMs = time.Since(start).Milliseconds()
			rec.Replayed = w.Header().Get("Idempotent-Replayed") == "true"
			rec.RequestBody, rec.ReqTruncated = req.buf.String(), req.truncated
			rec.ResponseBody, rec.RespTruncated = lw.resp.buf.String(), lw.resp.truncated
			c.requestLog.Add(*rec)
			log.Print(rec.line())
		}()
		next.ServeHTTP(lw, r)
	})
}

func (r RequestRecord) line() string {
	var b strings.Builder
	b.WriteString("[integration-api] ")
	b.WriteString(r.Method + " " + r.Path + " ")
	b.WriteString(strconv.Itoa(r.Status))
	if r.ErrorCode != "" {
		b.WriteString(" " + r.ErrorCode)
	}
	b.WriteString(" " + strconv.FormatInt(r.DurationMs, 10) + "ms")
	if r.Client != "" {
		b.WriteString(" client=" + r.Client)
	} else {
		b.WriteString(" client=-")
	}
	b.WriteString(" ip=" + r.IP)
	if r.KeyHint != "" {
		b.WriteString(" key=" + r.KeyHint + "…")
	}
	if r.Replayed {
		b.WriteString(" replayed")
	}
	return b.String()
}
