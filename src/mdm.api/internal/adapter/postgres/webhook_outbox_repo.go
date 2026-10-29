package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WebhookOutbox is the queue of reservation events waiting to be sent to the
// dispatch system (migration 044).
type WebhookOutbox struct{ pool *pgxpool.Pool }

func NewWebhookOutbox(pool *pgxpool.Pool) *WebhookOutbox { return &WebhookOutbox{pool: pool} }

// OutboxItem is an event due for delivery.
type OutboxItem struct {
	ID       int64
	EventID  string
	Body     []byte
	Attempts int // deliveries tried so far
}

// Enqueue queues an event. build gets the event's sequence number (to make its
// eventId) and returns the eventId and the exact JSON body to send.
func (o *WebhookOutbox) Enqueue(ctx context.Context, eventType, reservationID string,
	build func(seq int64) (eventID string, body []byte, err error)) error {
	var seq int64
	if err := o.pool.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('integration_webhook_outbox','id'))`).Scan(&seq); err != nil {
		return err
	}
	eventID, body, err := build(seq)
	if err != nil {
		return err
	}
	_, err = o.pool.Exec(ctx,
		`INSERT INTO integration_webhook_outbox (id, event_id, event_type, reservation_id, body) VALUES ($1,$2,$3,$4,$5)`,
		seq, eventID, eventType, reservationID, body)
	return err
}

// ClaimDue takes up to limit events that are due, oldest first, and leases them
// (they are not handed out again until the lease runs out). An event waits
// while an earlier event of the same reservation is still pending, so a
// reservation's events arrive in the order they happened.
func (o *WebhookOutbox) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]OutboxItem, error) {
	rows, err := o.pool.Query(ctx,
		`UPDATE integration_webhook_outbox o SET next_attempt_at = now() + make_interval(secs => $2)
		 WHERE o.id IN (
		     SELECT q.id FROM integration_webhook_outbox q
		     WHERE q.status = 'pending' AND q.next_attempt_at <= now()
		       AND NOT EXISTS (SELECT 1 FROM integration_webhook_outbox p
		                       WHERE p.reservation_id = q.reservation_id AND p.id < q.id AND p.status = 'pending')
		     ORDER BY q.id LIMIT $1 FOR UPDATE SKIP LOCKED)
		 RETURNING o.id, o.event_id, o.body, o.attempts`, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []OutboxItem
	for rows.Next() {
		var it OutboxItem
		if err := rows.Scan(&it.ID, &it.EventID, &it.Body, &it.Attempts); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (o *WebhookOutbox) MarkSent(ctx context.Context, id int64) error {
	_, err := o.pool.Exec(ctx,
		`UPDATE integration_webhook_outbox SET status='sent', attempts=attempts+1, sent_at=now(), last_error='' WHERE id=$1`, id)
	return err
}

// MarkRetry records a failed try and schedules the next one after delay.
func (o *WebhookOutbox) MarkRetry(ctx context.Context, id int64, delay time.Duration, errText string) error {
	_, err := o.pool.Exec(ctx,
		`UPDATE integration_webhook_outbox SET attempts=attempts+1, last_error=$3, next_attempt_at=now()+make_interval(secs => $2) WHERE id=$1`,
		id, delay.Seconds(), errText)
	return err
}

// MarkFailed gives the event up.
func (o *WebhookOutbox) MarkFailed(ctx context.Context, id int64, errText string) error {
	_, err := o.pool.Exec(ctx,
		`UPDATE integration_webhook_outbox SET status='failed', attempts=attempts+1, last_error=$2 WHERE id=$1`, id, errText)
	return err
}

// WebhookStats summarises the queue for the settings screen.
type WebhookStats struct {
	Pending       int
	Failed        int
	LastError     string
	LastSentAt    *time.Time
	OldestPending *time.Time
}

func (o *WebhookOutbox) Stats(ctx context.Context) (WebhookStats, error) {
	var s WebhookStats
	err := o.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE status='pending'), count(*) FILTER (WHERE status='failed'),
		        max(sent_at), min(created_at) FILTER (WHERE status='pending')
		 FROM integration_webhook_outbox`).Scan(&s.Pending, &s.Failed, &s.LastSentAt, &s.OldestPending)
	if err != nil {
		return s, err
	}
	// The most recent failure still on the books.
	err = o.pool.QueryRow(ctx,
		`SELECT last_error FROM integration_webhook_outbox
		 WHERE last_error <> '' AND status <> 'sent' ORDER BY id DESC LIMIT 1`).Scan(&s.LastError)
	if err != nil {
		s.LastError = ""
	}
	return s, nil
}

// RetryFailed puts every given-up event back in the queue; it returns how many.
func (o *WebhookOutbox) RetryFailed(ctx context.Context) (int64, error) {
	tag, err := o.pool.Exec(ctx,
		`UPDATE integration_webhook_outbox SET status='pending', attempts=0, next_attempt_at=now() WHERE status='failed'`)
	return tag.RowsAffected(), err
}

// Prune drops delivered events older than keep.
func (o *WebhookOutbox) Prune(ctx context.Context, keep time.Duration) error {
	_, err := o.pool.Exec(ctx,
		`DELETE FROM integration_webhook_outbox WHERE status='sent' AND sent_at < now() - make_interval(secs => $1)`, keep.Seconds())
	return err
}
