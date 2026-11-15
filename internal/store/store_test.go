package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/poomyrd-hue/meeting-room-booking/internal/booking"
)

var ctx = context.Background()

// at returns 2026-11-17 h:m UTC.
func at(h, m int) time.Time { return time.Date(2026, 11, 17, h, m, 0, 0, time.UTC) }

var now = time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)

// newStore uses a temp file (not :memory:) because each in-memory connection is a separate DB.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	rooms := []booking.Room{
		{ID: "r1", Name: "Alpha", Capacity: 8, Floor: 1},
		{ID: "r2", Name: "Beta", Capacity: 4, Floor: 2},
	}
	if err := s.SeedRooms(ctx, rooms); err != nil {
		t.Fatal(err)
	}
	return s
}

func book(t *testing.T, s *Store, room, user string, start, end time.Time) (*Booking, error) {
	t.Helper()
	b := &Booking{RoomID: room, BookerID: user, Start: start, End: end, Attendees: 2}
	return b, s.CreateBooking(ctx, b, now)
}

func TestSeedRoomsUpsert(t *testing.T) {
	s := newStore(t)
	if err := s.SeedRooms(ctx, []booking.Room{{ID: "r1", Name: "Alpha", Capacity: 12, Floor: 1}}); err != nil {
		t.Fatal(err)
	}
	rooms, err := s.Rooms(ctx)
	if err != nil || len(rooms) != 2 {
		t.Fatalf("rooms=%v err=%v, want 2 rooms", rooms, err)
	}
	if rooms[0].Capacity != 12 {
		t.Fatalf("r1 capacity = %d, want 12 after upsert", rooms[0].Capacity)
	}
	if _, err := s.Room(ctx, "nope"); !errors.Is(err, booking.ErrRoomNotFound) {
		t.Fatalf("got %v, want room_not_found", err)
	}
}

func TestCreateBookingConflicts(t *testing.T) {
	tests := []struct {
		name       string
		room       string
		start, end time.Time
		cancelOld  bool
		want       error
	}{
		{"overlap tail", "r1", at(10, 30), at(11, 30), false, booking.ErrTimeConflict},
		{"overlap head", "r1", at(9, 0), at(10, 30), false, booking.ErrTimeConflict},
		{"identical", "r1", at(10, 0), at(11, 0), false, booking.ErrTimeConflict},
		{"encloses", "r1", at(9, 30), at(11, 30), false, booking.ErrTimeConflict},
		{"inside", "r1", at(10, 15), at(10, 45), false, booking.ErrTimeConflict},
		{"starts when old ends", "r1", at(11, 0), at(12, 0), false, nil},
		{"ends when old starts", "r1", at(9, 0), at(10, 0), false, nil},
		{"other room", "r2", at(10, 0), at(11, 0), false, nil},
		{"after cancel", "r1", at(10, 0), at(11, 0), true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			old, err := book(t, s, "r1", "alice", at(10, 0), at(11, 0))
			if err != nil {
				t.Fatal(err)
			}
			if tc.cancelOld {
				if err := s.Cancel(ctx, old.ID, "alice", now); err != nil {
					t.Fatal(err)
				}
			}
			_, err = book(t, s, tc.room, "bob", tc.start, tc.end)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCreateBookingConcurrent(t *testing.T) {
	s := newStore(t)
	const n = 20
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = book(t, s, "r1", "u", at(10, 0), at(11, 0))
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

func TestCancel(t *testing.T) {
	future := at(10, 0)
	tests := []struct {
		name   string
		id     string // "" = the booking created in the test
		user   string
		now    time.Time
		cancel bool // cancel once before the call under test
		want   error
	}{
		{"ok", "", "alice", now, false, nil},
		{"not booker", "", "bob", now, false, booking.ErrNotBooker},
		{"unknown id", "missing", "alice", now, false, booking.ErrBookingNotFound},
		{"already cancelled", "", "alice", now, true, booking.ErrAlreadyCancelled},
		{"already started", "", "alice", future.Add(time.Minute), false, booking.ErrAlreadyStarted},
		{"starts exactly now", "", "alice", future, false, booking.ErrAlreadyStarted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			b, err := book(t, s, "r1", "alice", future, at(11, 0))
			if err != nil {
				t.Fatal(err)
			}
			id := tc.id
			if id == "" {
				id = b.ID
			}
			if tc.cancel {
				if err := s.Cancel(ctx, id, "alice", now); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Cancel(ctx, id, tc.user, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBookingsBetween(t *testing.T) {
	s := newStore(t)
	book(t, s, "r1", "a", at(14, 0), at(15, 0))
	early, _ := book(t, s, "r1", "a", at(9, 0), at(10, 0))
	book(t, s, "r2", "a", at(9, 0), at(10, 0))
	cancelled, _ := book(t, s, "r1", "a", at(11, 0), at(12, 0))
	if err := s.Cancel(ctx, cancelled.ID, "a", now); err != nil {
		t.Fatal(err)
	}
	next := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	book(t, s, "r1", "a", next, next.Add(time.Hour))

	got, err := s.BookingsBetween(ctx, "r1", at(0, 0), at(0, 0).AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != early.ID || !got[0].Start.Equal(at(9, 0)) || got[1].Start.Before(got[0].Start) {
		t.Fatalf("got %+v, want 2 confirmed r1 bookings in order, earliest %s", got, early.ID)
	}
}

func TestAvailableRooms(t *testing.T) {
	s := newStore(t) // r1 cap 8, r2 cap 4
	book(t, s, "r1", "a", at(10, 0), at(11, 0))
	tests := []struct {
		name       string
		start, end time.Time
		attendees  int
		want       []string
	}{
		{"r1 busy", at(10, 30), at(11, 30), 2, []string{"r2"}},
		{"both free after", at(11, 0), at(12, 0), 2, []string{"r1", "r2"}},
		{"capacity filters r2", at(11, 0), at(12, 0), 6, []string{"r1"}},
		{"none fit", at(11, 0), at(12, 0), 20, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rooms, err := s.AvailableRooms(ctx, tc.start, tc.end, tc.attendees)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, r := range rooms {
				ids = append(ids, r.ID)
			}
			if len(ids) != len(tc.want) {
				t.Fatalf("got %v, want %v", ids, tc.want)
			}
			for i := range ids {
				if ids[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", ids, tc.want)
				}
			}
		})
	}
}

func TestLoadRooms(t *testing.T) {
	rooms, err := LoadRooms(filepath.Join("..", "..", "rooms.json"))
	if err != nil || len(rooms) == 0 {
		t.Fatalf("rooms=%v err=%v, want the seed rooms", rooms, err)
	}
	if rooms[0].ID == "" || rooms[0].Capacity < 1 {
		t.Fatalf("first room not parsed: %+v", rooms[0])
	}
}
