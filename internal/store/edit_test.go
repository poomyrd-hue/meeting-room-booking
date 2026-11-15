package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/poomyrd-hue/meeting-room-booking/internal/booking"
)

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

func tp(t time.Time) *time.Time { return &t }
func ip(n int) *int             { return &n }

func TestEditBooking(t *testing.T) {
	// Alice's booking is Tuesday 2026-11-17 03:00-04:00 UTC (10:00-11:00 Bangkok); "now" is Monday 09:00 Bangkok.
	tests := []struct {
		name   string
		id     string // "" = alice's booking under test
		user   string
		clock  time.Time
		cancel bool
		patch  Patch
		other  [2]time.Time // a second booking by bob in r1; zero = none
		otherX bool         // cancel that second booking
		want   error
		wantS  time.Time // expected after success
		wantE  time.Time
		wantN  int
	}{
		{name: "moves into free slot", patch: Patch{Start: tp(at(5, 0)), End: tp(at(6, 0))}, wantS: at(5, 0), wantE: at(6, 0), wantN: 2},
		{name: "extends over own old slot", patch: Patch{End: tp(at(5, 0))}, wantS: at(3, 0), wantE: at(5, 0), wantN: 2},
		{name: "shifts over own old slot", patch: Patch{Start: tp(at(3, 30)), End: tp(at(4, 30))}, wantS: at(3, 30), wantE: at(4, 30), wantN: 2},
		{name: "attendees only", patch: Patch{Attendees: ip(6)}, wantS: at(3, 0), wantE: at(4, 0), wantN: 6},
		{name: "over capacity", patch: Patch{Attendees: ip(9)}, want: booking.ErrOverCapacity},
		{name: "bad slot", patch: Patch{End: tp(at(4, 15))}, want: booking.ErrInvalidSlot},
		{name: "conflict with other", patch: Patch{End: tp(at(5, 0))}, other: [2]time.Time{at(4, 30), at(5, 30)}, want: booking.ErrTimeConflict},
		{name: "back to back is fine", patch: Patch{End: tp(at(5, 0))}, other: [2]time.Time{at(5, 0), at(6, 0)}, wantS: at(3, 0), wantE: at(5, 0), wantN: 2},
		{name: "ignores cancelled", patch: Patch{End: tp(at(5, 0))}, other: [2]time.Time{at(4, 30), at(5, 30)}, otherX: true, wantS: at(3, 0), wantE: at(5, 0), wantN: 2},
		{name: "not booker", user: "bob", patch: Patch{Attendees: ip(3)}, want: booking.ErrNotBooker},
		{name: "unknown id", id: "missing", patch: Patch{Attendees: ip(3)}, want: booking.ErrBookingNotFound},
		{name: "cancelled", cancel: true, patch: Patch{Attendees: ip(3)}, want: booking.ErrAlreadyCancelled},
		{name: "started", clock: at(3, 0), patch: Patch{Attendees: ip(3)}, want: booking.ErrAlreadyStarted},
		// Status checks come before validation: a started booking reports already_started, not in_the_past.
		{name: "started beats validation", clock: at(3, 30), patch: Patch{Start: tp(at(3, 0))}, want: booking.ErrAlreadyStarted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			b, err := book(t, s, "r1", "alice", at(3, 0), at(4, 0))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.other[0].IsZero() {
				o, err := book(t, s, "r1", "bob", tc.other[0], tc.other[1])
				if err != nil {
					t.Fatal(err)
				}
				if tc.otherX {
					if err := s.Cancel(ctx, o.ID, "bob", now); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.cancel {
				if err := s.Cancel(ctx, b.ID, "alice", now); err != nil {
					t.Fatal(err)
				}
			}
			id, user, clock := tc.id, tc.user, tc.clock
			if id == "" {
				id = b.ID
			}
			if user == "" {
				user = "alice"
			}
			if clock.IsZero() {
				clock = now
			}
			got, err := s.Edit(ctx, id, user, tc.patch, fixedClock(clock))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				return
			}
			if !got.Start.Equal(tc.wantS) || !got.End.Equal(tc.wantE) || got.Attendees != tc.wantN ||
				got.ID != b.ID || got.BookerID != "alice" || got.Status != "confirmed" || !got.CreatedAt.Equal(b.CreatedAt) {
				t.Fatalf("got %+v, want %v-%v n=%d with id/booker/status/created_at unchanged", got, tc.wantS, tc.wantE, tc.wantN)
			}
			// What was returned is what was stored.
			bs, _ := s.BookingsBetween(ctx, "r1", at(0, 0), at(23, 0))
			found := false
			for _, x := range bs {
				if x.ID == b.ID && x.Start.Equal(tc.wantS) && x.End.Equal(tc.wantE) && x.Attendees == tc.wantN {
					found = true
				}
			}
			if !found {
				t.Fatalf("stored %+v does not contain the edited booking", bs)
			}
		})
	}
}

func TestEditFailureLeavesBookingUnchanged(t *testing.T) {
	s := newStore(t)
	b, _ := book(t, s, "r1", "alice", at(3, 0), at(4, 0))
	if _, err := s.Edit(ctx, b.ID, "alice", Patch{Attendees: ip(9)}, fixedClock(now)); !errors.Is(err, booking.ErrOverCapacity) {
		t.Fatal(err)
	}
	bs, _ := s.BookingsBetween(ctx, "r1", at(0, 0), at(23, 0))
	if len(bs) != 1 || bs[0].Attendees != 2 {
		t.Fatalf("stored %+v, want untouched", bs)
	}
}

// Many bookings try to move into the same free slot: exactly one may win.
func TestEditConcurrentNoDoubleBook(t *testing.T) {
	s := newStore(t)
	const n = 10
	ids := make([]string, n)
	for i := range ids {
		off := time.Duration(i) * 30 * time.Minute
		b, err := book(t, s, "r1", "alice", at(1, 0).Add(off), at(1, 30).Add(off))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = b.ID
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Edit(ctx, ids[i], "alice", Patch{Start: tp(at(9, 0)), End: tp(at(9, 30))}, fixedClock(now))
		}()
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, booking.ErrTimeConflict):
			conflict++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || conflict != n-1 {
		t.Fatalf("ok=%d conflict=%d, want 1 and %d", ok, conflict, n-1)
	}
}
