package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poomyrd-hue/meeting-room-booking/internal/booking"
	"github.com/poomyrd-hue/meeting-room-booking/internal/store"
)

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

// Monday 2026-11-16 09:00 Bangkok.
var now = time.Date(2026, 11, 16, 9, 0, 0, 0, booking.Bangkok)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	err = s.SeedRooms(t.Context(), []booking.Room{
		{ID: "r1", Name: "Alpha", Capacity: 8, Floor: 1},
		{ID: "r2", Name: "Beta", Capacity: 4, Floor: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(t *testing.T, h http.Handler, method, path, body string, hdr ...string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		var v any
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatalf("%s %s: body not JSON: %q", method, path, rec.Body.String())
		}
		out, _ = v.(map[string]any)
		if out == nil {
			out = map[string]any{"list": v}
		}
	}
	return rec.Code, out
}

func bookingBody(room, user, start, end string, n int) string {
	b, _ := json.Marshal(map[string]any{"room_id": room, "user_id": user, "start": start, "end": end, "attendees": n})
	return string(b)
}

const (
	d17at10 = "2026-11-17T10:00:00+07:00"
	d17at11 = "2026-11-17T11:00:00+07:00"
)

func TestCreateBooking(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"ok", bookingBody("r1", "alice", d17at10, d17at11, 4), 201, ""},
		{"unknown room", bookingBody("nope", "alice", d17at10, d17at11, 4), 404, "room_not_found"},
		{"bad slot", bookingBody("r1", "alice", "2026-11-17T10:15:00+07:00", d17at11, 4), 422, "invalid_slot"},
		{"too long", bookingBody("r1", "alice", d17at10, "2026-11-17T15:00:00+07:00", 4), 422, "invalid_duration"},
		{"outside hours", bookingBody("r1", "alice", "2026-11-17T07:00:00+07:00", "2026-11-17T08:00:00+07:00", 4), 422, "outside_business_hours"},
		{"in the past", bookingBody("r1", "alice", "2026-11-16T08:00:00+07:00", "2026-11-16T09:00:00+07:00", 4), 422, "in_the_past"},
		{"too far ahead", bookingBody("r1", "alice", "2027-01-15T09:30:00+07:00", "2027-01-15T10:30:00+07:00", 4), 422, "too_far_ahead"},
		{"over capacity", bookingBody("r1", "alice", d17at10, d17at11, 9), 422, "over_capacity"},
		{"missing user", bookingBody("r1", "", d17at10, d17at11, 4), 422, "invalid_request"},
		{"blank user", bookingBody("r1", "   ", d17at10, d17at11, 4), 422, "invalid_request"},
		{"user too long", bookingBody("r1", strings.Repeat("a", 61), d17at10, d17at11, 4), 422, "invalid_request"},
		{"zero attendees", bookingBody("r1", "alice", d17at10, d17at11, 0), 422, "invalid_request"},
		{"negative attendees", bookingBody("r1", "alice", d17at10, d17at11, -1), 422, "invalid_request"},
		{"missing start", `{"room_id":"r1","user_id":"a","end":"` + d17at11 + `","attendees":2}`, 422, "invalid_request"},
		{"bad json", `{`, 422, "invalid_request"},
		{"unknown field", `{"room_id":"r1","user_id":"a","start":"` + d17at10 + `","end":"` + d17at11 + `","attendees":2,"x":1}`, 422, "invalid_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(newStore(t), fixedClock(now))
			code, body := do(t, h, "POST", "/bookings", tc.body)
			if code != tc.wantCode {
				t.Fatalf("status %d (%v), want %d", code, body, tc.wantCode)
			}
			if tc.wantErr != "" && body["code"] != tc.wantErr {
				t.Fatalf("code %v, want %s", body["code"], tc.wantErr)
			}
			if tc.wantCode == 201 && (body["id"] == "" || body["status"] != "confirmed" || body["start"] != "2026-11-17T03:00:00Z") {
				t.Fatalf("unexpected booking: %v", body)
			}
		})
	}
}

func TestCreateBookingConflict(t *testing.T) {
	h := New(newStore(t), fixedClock(now))
	if code, _ := do(t, h, "POST", "/bookings", bookingBody("r1", "alice", d17at10, d17at11, 2)); code != 201 {
		t.Fatalf("first booking: %d", code)
	}
	code, body := do(t, h, "POST", "/bookings", bookingBody("r1", "bob", "2026-11-17T10:30:00+07:00", "2026-11-17T11:30:00+07:00", 2))
	if code != 409 || body["code"] != "time_conflict" {
		t.Fatalf("got %d %v, want 409 time_conflict", code, body)
	}
	if code, _ := do(t, h, "POST", "/bookings", bookingBody("r1", "bob", d17at11, "2026-11-17T12:00:00+07:00", 2)); code != 201 {
		t.Fatalf("back-to-back booking: %d, want 201", code)
	}
}

func TestCancel(t *testing.T) {
	s := newStore(t)
	h := New(s, fixedClock(now))
	create := func() string {
		_, b := do(t, h, "POST", "/bookings", bookingBody("r1", "alice", d17at10, d17at11, 2))
		return b["id"].(string)
	}

	id := create()
	tests := []struct {
		name     string
		h        http.Handler
		id       string
		hdr      []string
		wantCode int
		wantErr  string
	}{
		{"missing header", h, id, nil, 422, "invalid_request"},
		{"not booker", h, id, []string{"X-User-Id", "bob"}, 403, "not_booker"},
		{"unknown", h, "missing", []string{"X-User-Id", "alice"}, 404, "booking_not_found"},
		{"already started", New(s, fixedClock(time.Date(2026, 11, 17, 10, 30, 0, 0, booking.Bangkok))), id, []string{"X-User-Id", "alice"}, 409, "already_started"},
		{"ok", h, id, []string{"X-User-Id", "alice"}, 200, ""},
		{"twice", h, id, []string{"X-User-Id", "alice"}, 409, "already_cancelled"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, body := do(t, tc.h, "POST", "/bookings/"+tc.id+"/cancel", "", tc.hdr...)
			if code != tc.wantCode || (tc.wantErr != "" && body["code"] != tc.wantErr) {
				t.Fatalf("got %d %v, want %d %s", code, body, tc.wantCode, tc.wantErr)
			}
		})
	}

	// The slot is free again after cancelling.
	if code, _ := do(t, h, "POST", "/bookings", bookingBody("r1", "bob", d17at10, d17at11, 2)); code != 201 {
		t.Fatalf("rebook after cancel: %d, want 201", code)
	}
}

func TestListRooms(t *testing.T) {
	h := New(newStore(t), fixedClock(now))
	code, body := do(t, h, "GET", "/rooms", "")
	list, _ := body["list"].([]any)
	if code != 200 || len(list) != 2 {
		t.Fatalf("got %d %v, want 200 with 2 rooms", code, body)
	}
	if first := list[0].(map[string]any); first["id"] != "r1" || first["capacity"] != float64(8) {
		t.Fatalf("first room: %v", first)
	}
}

func TestAvailableRooms(t *testing.T) {
	h := New(newStore(t), fixedClock(now))
	do(t, h, "POST", "/bookings", bookingBody("r1", "alice", d17at10, d17at11, 2))

	ids := func(path string) ([]string, int) {
		code, body := do(t, h, "GET", path, "")
		var out []string
		for _, r := range body["list"].([]any) {
			out = append(out, r.(map[string]any)["id"].(string))
		}
		return out, code
	}
	q := "?start=" + "2026-11-17T10:00:00%2B07:00" + "&end=" + "2026-11-17T11:00:00%2B07:00"
	if got, code := ids("/rooms/available" + q + "&attendees=2"); code != 200 || len(got) != 1 || got[0] != "r2" {
		t.Fatalf("got %v %d, want [r2]", got, code)
	}
	if got, _ := ids("/rooms/available" + q + "&attendees=6"); len(got) != 0 {
		t.Fatalf("got %v, want none (r1 busy, r2 too small)", got)
	}
	for _, bad := range []string{"", "?start=x&end=y&attendees=2", q + "&attendees=0", q, "?start=2026-11-17T11:00:00%2B07:00&end=2026-11-17T10:00:00%2B07:00&attendees=2"} {
		if code, body := do(t, h, "GET", "/rooms/available"+bad, ""); code != 422 || body["code"] != "invalid_request" {
			t.Fatalf("query %q: got %d %v, want 422 invalid_request", bad, code, body)
		}
	}
}

func TestRoomBookingsByDate(t *testing.T) {
	h := New(newStore(t), fixedClock(now))
	do(t, h, "POST", "/bookings", bookingBody("r1", "alice", d17at10, d17at11, 2))
	do(t, h, "POST", "/bookings", bookingBody("r1", "alice", "2026-11-18T10:00:00+07:00", "2026-11-18T11:00:00+07:00", 2))

	code, body := do(t, h, "GET", "/rooms/r1/bookings?date=2026-11-17", "")
	list, _ := body["list"].([]any)
	if code != 200 || len(list) != 1 {
		t.Fatalf("got %d %v, want 1 booking on the 17th", code, body)
	}
	if code, body := do(t, h, "GET", "/rooms/r2/bookings?date=2026-11-17", ""); code != 200 || len(body["list"].([]any)) != 0 {
		t.Fatalf("empty day: got %d %v, want 200 and []", code, body)
	}
	if code, body := do(t, h, "GET", "/rooms/nope/bookings?date=2026-11-17", ""); code != 404 || body["code"] != "room_not_found" {
		t.Fatalf("got %d %v, want 404 room_not_found", code, body)
	}
	if code, body := do(t, h, "GET", "/rooms/r1/bookings?date=17-11-2026", ""); code != 422 || body["code"] != "invalid_request" {
		t.Fatalf("got %d %v, want 422 invalid_request", code, body)
	}
}

func TestUserIDIsTrimmedAndBounded(t *testing.T) {
	h := New(newStore(t), fixedClock(now))
	code, body := do(t, h, "POST", "/bookings", bookingBody("r1", "  alice  ", d17at10, d17at11, 2))
	if code != 201 || body["booker_id"] != "alice" {
		t.Fatalf("got %d %v, want 201 with booker_id alice", code, body)
	}
	if code, _ := do(t, h, "POST", "/bookings", bookingBody("r1", strings.Repeat("ก", 60), d17at11, "2026-11-17T12:00:00+07:00", 2)); code != 201 {
		t.Fatalf("60-character name: got %d, want 201 (limit counts characters, not bytes)", code)
	}
	id := body["id"].(string)
	if code, body := do(t, h, "POST", "/bookings/"+id+"/cancel", "", "X-User-Id", "  alice "); code != 200 {
		t.Fatalf("cancel with padded header: got %d %v, want 200", code, body)
	}
	if code, body := do(t, h, "POST", "/bookings/"+id+"/cancel", "", "X-User-Id", "   "); code != 422 || body["code"] != "invalid_request" {
		t.Fatalf("blank header: got %d %v, want 422 invalid_request", code, body)
	}
}
