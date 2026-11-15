// Package store persists rooms and bookings in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/poomyrd-hue/meeting-room-booking/internal/booking"
	_ "modernc.org/sqlite"
)

// timeLayout is fixed-width UTC, so string comparison in SQL orders times correctly.
const timeLayout = "2006-01-02T15:04:05Z"

const schema = `
CREATE TABLE IF NOT EXISTS rooms (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL,
	capacity INTEGER NOT NULL CHECK (capacity > 0),
	floor    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS bookings (
	id           TEXT PRIMARY KEY,
	room_id      TEXT NOT NULL REFERENCES rooms(id),
	booker_id    TEXT NOT NULL,
	start_utc    TEXT NOT NULL,
	end_utc      TEXT NOT NULL CHECK (end_utc > start_utc),
	attendees    INTEGER NOT NULL CHECK (attendees > 0),
	status       TEXT NOT NULL CHECK (status IN ('confirmed','cancelled')),
	created_at   TEXT NOT NULL,
	cancelled_at TEXT
);
CREATE INDEX IF NOT EXISTS bookings_room_time
	ON bookings(room_id, start_utc, end_utc) WHERE status = 'confirmed';
`

type Booking struct {
	ID          string
	RoomID      string
	BookerID    string
	Start       time.Time
	End         time.Time
	Attendees   int
	Status      string
	CreatedAt   time.Time
	CancelledAt *time.Time
}

type Store struct{ db *sql.DB }

// Open opens (creating if needed) the SQLite file at path.
// Every transaction starts with BEGIN IMMEDIATE so check-then-insert cannot interleave.
func Open(path string) (*Store, error) {
	dsn := "file:" + filepath.ToSlash(path) +
		"?_txlock=immediate&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// LoadRooms reads a rooms.json file: a JSON array of rooms.
func LoadRooms(path string) ([]booking.Room, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rooms []booking.Room
	if err := json.Unmarshal(b, &rooms); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rooms, nil
}

// SeedRooms inserts rooms, updating existing ones by id.
func (s *Store) SeedRooms(ctx context.Context, rooms []booking.Room) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, r := range rooms {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO rooms(id,name,capacity,floor) VALUES(?,?,?,?)
				 ON CONFLICT(id) DO UPDATE SET name=excluded.name, capacity=excluded.capacity, floor=excluded.floor`,
				r.ID, r.Name, r.Capacity, r.Floor); err != nil {
				return fmt.Errorf("seed room %q: %w", r.ID, err)
			}
		}
		return nil
	})
}

func (s *Store) Rooms(ctx context.Context) ([]booking.Room, error) {
	return s.queryRooms(ctx, `SELECT id,name,capacity,floor FROM rooms ORDER BY id`)
}

func (s *Store) Room(ctx context.Context, id string) (booking.Room, error) {
	var r booking.Room
	err := s.db.QueryRowContext(ctx, `SELECT id,name,capacity,floor FROM rooms WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.Capacity, &r.Floor)
	if errors.Is(err, sql.ErrNoRows) {
		return r, booking.ErrRoomNotFound
	}
	return r, err
}

// AvailableRooms returns rooms that fit attendees and have no confirmed booking overlapping [start, end).
func (s *Store) AvailableRooms(ctx context.Context, start, end time.Time, attendees int) ([]booking.Room, error) {
	return s.queryRooms(ctx,
		`SELECT id,name,capacity,floor FROM rooms r
		 WHERE capacity >= ?
		   AND NOT EXISTS (SELECT 1 FROM bookings b WHERE b.room_id=r.id AND b.status='confirmed'
		                   AND b.start_utc < ? AND b.end_utc > ?)
		 ORDER BY id`,
		attendees, fmtTime(end), fmtTime(start))
}

// CreateBooking assigns b.ID, b.Status and b.CreatedAt and stores it, or returns ErrTimeConflict.
// The caller must have validated the rules and that the room exists.
func (s *Store) CreateBooking(ctx context.Context, b *Booking, now time.Time) error {
	id, err := newID()
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM bookings WHERE room_id=? AND status='confirmed' AND start_utc < ? AND end_utc > ?`,
			b.RoomID, fmtTime(b.End), fmtTime(b.Start)).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return booking.ErrTimeConflict
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO bookings(id,room_id,booker_id,start_utc,end_utc,attendees,status,created_at)
			 VALUES(?,?,?,?,?,?, 'confirmed', ?)`,
			id, b.RoomID, b.BookerID, fmtTime(b.Start), fmtTime(b.End), b.Attendees, fmtTime(now)); err != nil {
			return err
		}
		b.ID, b.Status, b.CreatedAt = id, "confirmed", now.UTC()
		return nil
	})
}

// Cancel marks a booking cancelled. Only its booker may cancel, and only before it starts.
func (s *Store) Cancel(ctx context.Context, id, userID string, now time.Time) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var booker, status, start string
		err := tx.QueryRowContext(ctx, `SELECT booker_id,status,start_utc FROM bookings WHERE id=?`, id).
			Scan(&booker, &status, &start)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return booking.ErrBookingNotFound
		case err != nil:
			return err
		case booker != userID:
			return booking.ErrNotBooker
		case status == "cancelled":
			return booking.ErrAlreadyCancelled
		case start <= fmtTime(now):
			return booking.ErrAlreadyStarted
		}
		_, err = tx.ExecContext(ctx, `UPDATE bookings SET status='cancelled', cancelled_at=? WHERE id=?`, fmtTime(now), id)
		return err
	})
}

// BookingsBetween returns confirmed bookings of a room starting in [from, to), earliest first.
func (s *Store) BookingsBetween(ctx context.Context, roomID string, from, to time.Time) ([]Booking, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,room_id,booker_id,start_utc,end_utc,attendees,status,created_at,cancelled_at
		 FROM bookings WHERE room_id=? AND status='confirmed' AND start_utc >= ? AND start_utc < ?
		 ORDER BY start_utc`, roomID, fmtTime(from), fmtTime(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Booking
	for rows.Next() {
		var b Booking
		var start, end, created string
		var cancelled sql.NullString
		if err := rows.Scan(&b.ID, &b.RoomID, &b.BookerID, &start, &end, &b.Attendees, &b.Status, &created, &cancelled); err != nil {
			return nil, err
		}
		b.Start, b.End, b.CreatedAt = parseTime(start), parseTime(end), parseTime(created)
		if cancelled.Valid {
			t := parseTime(cancelled.String)
			b.CancelledAt = &t
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) queryRooms(ctx context.Context, q string, args ...any) ([]booking.Room, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []booking.Room
	for rows.Next() {
		var r booking.Room
		if err := rows.Scan(&r.ID, &r.Name, &r.Capacity, &r.Floor); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func fmtTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(timeLayout, s) // only ever reads strings written by fmtTime
	return t
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
