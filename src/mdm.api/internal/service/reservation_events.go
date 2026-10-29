package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/domain"
)

// Event types sent to the dispatch system (spec §7.6).
const (
	EventCreated       = "reservation.created"
	EventChanged       = "reservation.changed"
	EventCancelled     = "reservation.cancelled"
	EventStatusChanged = "reservation.status_changed"
)

// EventReservation is the reservation as the webhook describes it: the fields
// of spec §7.6, plus the borrower's email and name (extra fields are allowed).
type EventReservation struct {
	Status             string   `json:"status"`
	ResourceID         string   `json:"resourceId"`
	BorrowerEmployeeID *string  `json:"borrowerEmployeeId"`
	BorrowerEmail      string   `json:"borrowerEmail,omitempty"`
	BorrowerName       string   `json:"borrowerName,omitempty"`
	Passengers         []string `json:"passengers"`
	StartAt            string   `json:"startAt"`
	EndAt              *string  `json:"endAt"`
}

// MDMEvent is the body of one webhook call.
type MDMEvent struct {
	EventID       string            `json:"eventId"`
	EventType     string            `json:"eventType"`
	OccurredAt    string            `json:"occurredAt"`
	ReservationID string            `json:"reservationId"`
	ExternalRef   *string           `json:"externalRef"`
	ChangedBy     string            `json:"changedBy,omitempty"`
	Note          string            `json:"note,omitempty"`
	Reservation   EventReservation  `json:"reservation"`
	Previous      *EventReservation `json:"previous"`
}

func eventReservation(r *domain.Reservation) EventReservation {
	v := NewReservationView(r)
	return EventReservation{
		Status: v.Status, ResourceID: v.ResourceID, BorrowerEmployeeID: v.BorrowerEmployeeID,
		BorrowerEmail: v.BorrowerEmail, BorrowerName: v.BorrowerName,
		Passengers: v.Passengers, StartAt: v.StartAt, EndAt: v.EndAt,
	}
}

// ClassifyChange decides what a change to a reservation is, from how it looked
// before (nil: did not exist) and after (nil: no longer exists):
//
//	new                               created
//	cancelled / deleted               cancelled (a deleted one is reported with status CANCELLED)
//	time, borrower, riders, car       changed
//	only the status (out, back)       status_changed
//	nothing                           no event
//
// The returned reservation is the "after" to report.
func ClassifyChange(before, after *domain.Reservation) (eventType string, report *domain.Reservation, ok bool) {
	switch {
	case before == nil && after == nil:
		return "", nil, false
	case before == nil:
		return EventCreated, after, true
	case after == nil:
		if before.Status == domain.ReservationCancelled {
			return "", nil, false // was already cancelled
		}
		gone := *before
		gone.Status = domain.ReservationCancelled
		return EventCancelled, &gone, true
	case after.Status == domain.ReservationCancelled:
		if before.Status == domain.ReservationCancelled {
			return "", nil, false
		}
		return EventCancelled, after, true
	}
	b, a := eventReservation(before), eventReservation(after)
	if reflect.DeepEqual(b, a) {
		return "", nil, false
	}
	b.Status, a.Status = "", ""
	if reflect.DeepEqual(b, a) {
		return EventStatusChanged, after, true
	}
	return EventChanged, after, true
}

// BuildEvent assembles the event for a classified change.
func BuildEvent(eventID, eventType string, before, report *domain.Reservation, changedBy, note string, now time.Time) MDMEvent {
	ev := MDMEvent{
		EventID: eventID, EventType: eventType, OccurredAt: domain.FormatAPITime(now),
		ReservationID: domain.ReservationID(report.RentalNumber), ExternalRef: report.ExternalRef,
		ChangedBy: changedBy, Note: note, Reservation: eventReservation(report),
	}
	if before != nil {
		p := eventReservation(before)
		ev.Previous = &p
	}
	return ev
}

// ReservationEvents turns changes made in the MDM web UI into queued webhook
// events. Changes made through the integration API never come here — the
// dispatch system already knows about those. All methods are safe on a nil
// receiver, which is how the feature is switched off.
type ReservationEvents struct {
	store  *postgres.ReservationRepo
	outbox *postgres.WebhookOutbox
	now    func() time.Time
}

func NewReservationEvents(store *postgres.ReservationRepo, outbox *postgres.WebhookOutbox) *ReservationEvents {
	return &ReservationEvents{store: store, outbox: outbox, now: time.Now}
}

// Snapshot captures a reservation before a change; nil if the rental is not a
// reservation of an integration resource (or events are off).
func (e *ReservationEvents) Snapshot(ctx context.Context, rentalNumber int) *domain.Reservation {
	if e == nil || rentalNumber <= 0 {
		return nil
	}
	r, err := e.store.Get(ctx, rentalNumber)
	if err != nil {
		log.Printf("[mdm-webhook] snapshot R-%d: %v", rentalNumber, err)
		return nil
	}
	return r
}

// Changed queues an event if the reservation differs from the snapshot taken
// before the change. Failures are logged, never returned: a webhook problem
// must not fail the operator's action.
func (e *ReservationEvents) Changed(ctx context.Context, rentalNumber int, before *domain.Reservation, changedBy, note string) {
	if e == nil || rentalNumber <= 0 {
		return
	}
	after, err := e.store.Get(ctx, rentalNumber)
	if err != nil {
		log.Printf("[mdm-webhook] reload R-%d: %v", rentalNumber, err)
		return
	}
	eventType, report, ok := ClassifyChange(before, after)
	if !ok {
		return
	}
	now := e.now()
	err = e.outbox.Enqueue(ctx, eventType, domain.ReservationID(rentalNumber), func(seq int64) (string, []byte, error) {
		id := fmt.Sprintf("mdm-evt-%s-%06d", now.In(domain.TaipeiZone).Format("20060102"), seq)
		body, err := json.Marshal(BuildEvent(id, eventType, before, report, changedBy, note, now))
		return id, body, err
	})
	if err != nil {
		log.Printf("[mdm-webhook] queue %s for R-%d: %v", eventType, rentalNumber, err)
	}
}
