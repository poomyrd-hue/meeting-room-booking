package booking

import (
	"errors"
	"testing"
	"time"
)

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

// at builds a Bangkok-local time; day 16 = Monday 2026-11-16 (the fixed "now" day).
func at(month time.Month, day, hour, min int) time.Time {
	return time.Date(2026, month, day, hour, min, 0, 0, Bangkok)
}

func TestValidate(t *testing.T) {
	now := at(time.November, 16, 9, 0) // Monday 09:00
	if now.Weekday() != time.Monday {
		t.Fatalf("test fixture: want Monday, got %v", now.Weekday())
	}
	room := Room{ID: "r1", Capacity: 8}

	nov := func(d, h, m int) time.Time { return at(time.November, d, h, m) }
	// 2027-01-15 (Friday) is exactly 60 days after now: 09:00 is allowed, 09:30 is not.
	jan := func(h, m int) time.Time { return time.Date(2027, time.January, 15, h, m, 0, 0, Bangkok) }
	req := func(s, e time.Time, n int) Request {
		return Request{RoomID: "r1", UserID: "u1", Start: s, End: e, Attendees: n}
	}

	tests := []struct {
		name string
		req  Request
		want error
	}{
		{"valid", req(nov(17, 10, 0), nov(17, 11, 0), 4), nil},
		{"valid min 30m", req(nov(17, 10, 0), nov(17, 10, 30), 1), nil},
		{"valid max 4h", req(nov(17, 10, 0), nov(17, 14, 0), 8), nil},
		{"valid ends exactly 20:00", req(nov(17, 19, 30), nov(17, 20, 0), 2), nil},
		{"valid starts exactly 08:00", req(nov(17, 8, 0), nov(17, 9, 0), 2), nil},
		{"valid start == now", req(nov(16, 9, 0), nov(16, 10, 0), 2), nil},
		{"valid exactly 60 days ahead", req(jan(9, 0), jan(10, 0), 2), nil},
		{"start not on slot", req(nov(17, 10, 15), nov(17, 11, 0), 2), ErrInvalidSlot},
		{"end not on slot", req(nov(17, 10, 0), nov(17, 10, 45), 2), ErrInvalidSlot},
		{"start has seconds", req(nov(17, 10, 0).Add(30*time.Second), nov(17, 11, 0), 2), ErrInvalidSlot},
		{"zero length", req(nov(17, 10, 0), nov(17, 10, 0), 2), ErrInvalidDuration},
		{"end before start", req(nov(17, 11, 0), nov(17, 10, 0), 2), ErrInvalidDuration},
		{"over 4h", req(nov(17, 10, 0), nov(17, 14, 30), 2), ErrInvalidDuration},
		{"before 08:00", req(nov(17, 7, 30), nov(17, 8, 30), 2), ErrOutsideBusinessHours},
		{"after 20:00", req(nov(17, 19, 30), nov(17, 20, 30), 2), ErrOutsideBusinessHours},
		{"saturday", req(nov(21, 10, 0), nov(21, 11, 0), 2), ErrOutsideBusinessHours},
		{"sunday", req(nov(22, 10, 0), nov(22, 11, 0), 2), ErrOutsideBusinessHours},
		{"crosses midnight", req(nov(20, 23, 0), nov(21, 1, 0), 2), ErrOutsideBusinessHours},
		{"in the past", req(nov(16, 8, 30), nov(16, 9, 30), 2), ErrInThePast},
		{"too far ahead", req(jan(9, 30), jan(10, 30), 2), ErrTooFarAhead},
		{"zero attendees", req(nov(17, 10, 0), nov(17, 11, 0), 0), ErrOverCapacity},
		{"over capacity", req(nov(17, 10, 0), nov(17, 11, 0), 9), ErrOverCapacity},
		{"slot error beats other errors", req(nov(21, 10, 15), nov(21, 10, 45), 99), ErrInvalidSlot},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Validate(tc.req, room, fixedClock(now))
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
