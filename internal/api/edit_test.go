package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/poomyrd-hue/meeting-room-booking/internal/booking"
)

func TestEditBooking(t *testing.T) {
	s := newStore(t)
	h := New(s, fixedClock(now))
	create := func(user, start, end string) string {
		_, b := do(t, h, "POST", "/bookings", bookingBody("r1", user, start, end, 4))
		return b["id"].(string)
	}
	id := create("alice", d17at10, d17at11)
	other := create("bob", "2026-11-17T13:00:00+07:00", "2026-11-17T14:00:00+07:00")
	started := New(s, fixedClock(time.Date(2026, 11, 17, 10, 30, 0, 0, booking.Bangkok)))
	alice := []string{"X-User-Id", "alice"}

	tests := []struct {
		name     string
		h        http.Handler
		id       string
		body     string
		hdr      []string
		wantCode int
		wantErr  string
	}{
		{"missing header", h, id, `{"attendees":3}`, nil, 422, "invalid_request"},
		{"blank header", h, id, `{"attendees":3}`, []string{"X-User-Id", "  "}, 422, "invalid_request"},
		{"empty body", h, id, ``, alice, 422, "invalid_request"},
		{"empty object", h, id, `{}`, alice, 422, "invalid_request"},
		{"unknown field", h, id, `{"room_id":"r2"}`, alice, 422, "invalid_request"},
		{"attendees zero", h, id, `{"attendees":0}`, alice, 422, "invalid_request"},
		{"bad time", h, id, `{"end":"soon"}`, alice, 422, "invalid_request"},
		{"unknown id", h, "missing", `{"attendees":3}`, alice, 404, "booking_not_found"},
		{"not booker", h, id, `{"attendees":3}`, []string{"X-User-Id", "bob"}, 403, "not_booker"},
		{"already started", started, id, `{"attendees":3}`, alice, 409, "already_started"},
		{"over capacity", h, id, `{"attendees":9}`, alice, 422, "over_capacity"},
		{"bad slot", h, id, `{"end":"2026-11-17T11:15:00+07:00"}`, alice, 422, "invalid_slot"},
		{"too long", h, id, `{"end":"2026-11-17T15:00:00+07:00"}`, alice, 422, "invalid_duration"},
		{"outside hours", h, id, `{"start":"2026-11-17T07:00:00+07:00","end":"2026-11-17T08:00:00+07:00"}`, alice, 422, "outside_business_hours"},
		{"in the past", h, id, `{"start":"2026-11-16T08:00:00+07:00","end":"2026-11-16T09:00:00+07:00"}`, alice, 422, "in_the_past"},
		{"conflict", h, id, `{"start":"2026-11-17T12:30:00+07:00","end":"2026-11-17T13:30:00+07:00"}`, alice, 409, "time_conflict"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, body := do(t, tc.h, "PATCH", "/bookings/"+tc.id, tc.body, tc.hdr...)
			if code != tc.wantCode || body["code"] != tc.wantErr {
				t.Fatalf("got %d %v, want %d %s", code, body, tc.wantCode, tc.wantErr)
			}
		})
	}

	// Partial update keeps the other fields; extending over its own old slot works; shape matches POST 201.
	code, body := do(t, h, "PATCH", "/bookings/"+id, `{"end":"2026-11-17T12:00:00+07:00"}`, alice...)
	want := map[string]any{"id": id, "room_id": "r1", "booker_id": "alice", "start": "2026-11-17T03:00:00Z",
		"end": "2026-11-17T05:00:00Z", "attendees": 4.0, "status": "confirmed"}
	if code != 200 || len(body) != len(want) {
		t.Fatalf("got %d %v, want 200 %v", code, body, want)
	}
	for k, v := range want {
		if body[k] != v {
			t.Fatalf("%s = %v, want %v (body %v)", k, body[k], v, body)
		}
	}
	// Shifting by 30 minutes overlaps its own old slot and must succeed; attendees survive.
	code, body = do(t, h, "PATCH", "/bookings/"+id, `{"start":"2026-11-17T10:30:00+07:00","end":"2026-11-17T12:30:00+07:00"}`, alice...)
	if code != 200 || body["attendees"] != 4.0 || body["start"] != "2026-11-17T03:30:00Z" {
		t.Fatalf("shift: %d %v", code, body)
	}
	// A cancelled booking no longer blocks.
	do(t, h, "POST", "/bookings/"+other+"/cancel", "", "X-User-Id", "bob")
	if code, body := do(t, h, "PATCH", "/bookings/"+id, `{"end":"2026-11-17T14:00:00+07:00"}`, alice...); code != 200 {
		t.Fatalf("over cancelled booking: %d %v", code, body)
	}
	if code, body := do(t, h, "PATCH", "/bookings/"+id, `{"attendees":8}`, alice...); code != 200 || body["attendees"] != 8.0 {
		t.Fatalf("attendees: %d %v", code, body)
	}
	// A cancelled booking cannot be edited.
	do(t, h, "POST", "/bookings/"+id+"/cancel", "", alice...)
	if code, body := do(t, h, "PATCH", "/bookings/"+id, `{"attendees":3}`, alice...); code != 409 || body["code"] != "already_cancelled" {
		t.Fatalf("cancelled: %d %v", code, body)
	}
}
