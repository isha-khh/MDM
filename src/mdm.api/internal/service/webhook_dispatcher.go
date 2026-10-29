package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
)

// RetryDelay is how long to wait after the nth failed delivery (1-based) before
// trying again: 1, 2, 4, 8, 16 minutes — five retries after the first try
// (spec §7.6). ok=false means give up.
func RetryDelay(failedTries int) (d time.Duration, ok bool) {
	if failedTries < 1 || failedTries > 5 {
		return 0, false
	}
	return time.Duration(1<<(failedTries-1)) * time.Minute, true
}

// SignWebhook is the X-Signature value: "sha256=" and the lowercase hex of
// HMAC-SHA256(secret, "{timestamp}.{body}") (spec §7.6).
func SignWebhook(secret, timestamp string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp))
	m.Write([]byte("."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// WebhookDispatcher delivers the queued events to the dispatch system.
type WebhookDispatcher struct {
	outbox *postgres.WebhookOutbox
	url    string
	secret string
	client *http.Client
	now    func() time.Time
	tick   time.Duration
}

func NewWebhookDispatcher(outbox *postgres.WebhookOutbox, url, secret string) *WebhookDispatcher {
	return &WebhookDispatcher{
		outbox: outbox, url: url, secret: secret,
		client: &http.Client{Timeout: 15 * time.Second},
		now:    time.Now, tick: 5 * time.Second,
	}
}

// Run delivers events until ctx is cancelled.
func (d *WebhookDispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.tick)
	defer t.Stop()
	lastPrune := time.Time{}
	for {
		d.DeliverDue(ctx)
		if d.now().Sub(lastPrune) > 6*time.Hour {
			d.outbox.Prune(ctx, 30*24*time.Hour)
			lastPrune = d.now()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// DeliverDue sends whatever is due, once. It returns how many events were tried.
func (d *WebhookDispatcher) DeliverDue(ctx context.Context) int {
	items, err := d.outbox.ClaimDue(ctx, 20, 2*time.Minute)
	if err != nil {
		log.Printf("[mdm-webhook] claim: %v", err)
		return 0
	}
	for _, it := range items {
		d.deliver(ctx, it)
	}
	return len(items)
}

func (d *WebhookDispatcher) deliver(ctx context.Context, it postgres.OutboxItem) {
	status, detail, err := d.post(ctx, it.Body)
	switch {
	case err == nil && status == http.StatusOK:
		if e := d.outbox.MarkSent(ctx, it.ID); e != nil {
			log.Printf("[mdm-webhook] mark sent %s: %v", it.EventID, e)
		}
		return
	case err == nil && status == http.StatusBadRequest:
		// The receiver rejected the event itself; sending it again changes nothing.
		d.fail(ctx, it, fmt.Sprintf("HTTP 400: %s", detail))
		return
	}
	reason := detail
	if err != nil {
		reason = err.Error()
	} else {
		reason = fmt.Sprintf("HTTP %d: %s", status, detail)
	}
	if delay, ok := RetryDelay(it.Attempts + 1); ok {
		log.Printf("[mdm-webhook] %s failed (%s); retrying in %s", it.EventID, reason, delay)
		if e := d.outbox.MarkRetry(ctx, it.ID, delay, reason); e != nil {
			log.Printf("[mdm-webhook] mark retry %s: %v", it.EventID, e)
		}
		return
	}
	d.fail(ctx, it, reason)
}

func (d *WebhookDispatcher) fail(ctx context.Context, it postgres.OutboxItem, reason string) {
	log.Printf("[mdm-webhook] %s given up: %s", it.EventID, reason)
	if e := d.outbox.MarkFailed(ctx, it.ID, reason); e != nil {
		log.Printf("[mdm-webhook] mark failed %s: %v", it.EventID, e)
	}
}

// post sends one event, signed with the current time (the receiver rejects
// timestamps more than five minutes off, so a retry is signed afresh; the body
// and eventId stay the same).
func (d *WebhookDispatcher) post(ctx context.Context, body []byte) (status int, detail string, err error) {
	ts := strconv.FormatInt(d.now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mdm-webhook/1")
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", SignWebhook(d.secret, ts, body))
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return resp.StatusCode, string(b), nil
}
