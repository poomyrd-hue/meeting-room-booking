// Package api exposes the booking rules and store over HTTP/JSON.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/poomyrd-hue/meeting-room-booking/internal/booking"
	"github.com/poomyrd-hue/meeting-room-booking/internal/store"
)

const (
	codeInvalidRequest booking.Error = "invalid_request"
	maxBody                          = 1 << 20
	maxUserLen                       = 60 // characters; same limit as the UI name field
)

var statusOf = map[booking.Error]int{
	booking.ErrInvalidSlot:          http.StatusUnprocessableEntity,
	booking.ErrInvalidDuration:      http.StatusUnprocessableEntity,
	booking.ErrOutsideBusinessHours: http.StatusUnprocessableEntity,
	booking.ErrInThePast:            http.StatusUnprocessableEntity,
	booking.ErrTooFarAhead:          http.StatusUnprocessableEntity,
	booking.ErrOverCapacity:         http.StatusUnprocessableEntity,
	codeInvalidRequest:              http.StatusUnprocessableEntity,
	booking.ErrTimeConflict:         http.StatusConflict,
	booking.ErrAlreadyCancelled:     http.StatusConflict,
	booking.ErrAlreadyStarted:       http.StatusConflict,
	booking.ErrNotBooker:            http.StatusForbidden,
	booking.ErrRoomNotFound:         http.StatusNotFound,
	booking.ErrBookingNotFound:      http.StatusNotFound,
}

type server struct {
	store *store.Store
	clock booking.Clock
}

// New returns the HTTP handler for the booking API.
func New(s *store.Store, c booking.Clock) http.Handler {
	srv := &server{store: s, clock: c}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rooms", srv.listRooms)
	mux.HandleFunc("GET /rooms/available", srv.availableRooms)
	mux.HandleFunc("GET /rooms/{id}/bookings", srv.roomBookings)
	mux.HandleFunc("POST /bookings", srv.createBooking)
	mux.HandleFunc("POST /bookings/{id}/cancel", srv.cancelBooking)
	return mux
}

func (s *server) listRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.store.Rooms(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(rooms))
}

func (s *server) availableRooms(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, err1 := time.Parse(time.RFC3339, q.Get("start"))
	end, err2 := time.Parse(time.RFC3339, q.Get("end"))
	n, err3 := strconv.Atoi(q.Get("attendees"))
	if err1 != nil || err2 != nil || err3 != nil || !end.After(start) || n < 1 {
		writeError(w, codeInvalidRequest)
		return
	}
	rooms, err := s.store.AvailableRooms(r.Context(), start, end, n)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(rooms))
}

func (s *server) roomBookings(w http.ResponseWriter, r *http.Request) {
	day, err := time.ParseInLocation(time.DateOnly, r.URL.Query().Get("date"), booking.Bangkok)
	if err != nil {
		writeError(w, codeInvalidRequest)
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.Room(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	bs, err := s.store.BookingsBetween(r.Context(), id, day, day.AddDate(0, 0, 1))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]bookingJSON, 0, len(bs))
	for _, b := range bs {
		out = append(out, toJSON(b))
	}
	writeJSON(w, http.StatusOK, out)
}

type createRequest struct {
	RoomID    string    `json:"room_id"`
	UserID    string    `json:"user_id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Attendees int       `json:"attendees"`
}

func (s *server) createBooking(w http.ResponseWriter, r *http.Request) {
	var in createRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(&in)
	user, ok := cleanUser(in.UserID)
	if err != nil || in.RoomID == "" || !ok || in.Start.IsZero() || in.End.IsZero() || in.Attendees < 1 {
		writeError(w, codeInvalidRequest)
		return
	}
	room, err := s.store.Room(r.Context(), in.RoomID)
	if err != nil {
		writeError(w, err)
		return
	}
	req := booking.Request{RoomID: in.RoomID, UserID: user, Start: in.Start, End: in.End, Attendees: in.Attendees}
	if err := booking.Validate(req, room, s.clock); err != nil {
		writeError(w, err)
		return
	}
	b := store.Booking{RoomID: in.RoomID, BookerID: user, Start: in.Start, End: in.End, Attendees: in.Attendees}
	if err := s.store.CreateBooking(r.Context(), &b, s.clock.Now()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toJSON(b))
}

func (s *server) cancelBooking(w http.ResponseWriter, r *http.Request) {
	user, ok := cleanUser(r.Header.Get("X-User-Id"))
	if !ok {
		writeError(w, codeInvalidRequest)
		return
	}
	id := r.PathValue("id")
	if err := s.store.Cancel(r.Context(), id, user, s.clock.Now()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "cancelled"})
}

// cleanUser trims the user id and reports whether it is usable: non-empty and at most maxUserLen characters.
func cleanUser(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n > 0 && n <= maxUserLen
}

type bookingJSON struct {
	ID        string    `json:"id"`
	RoomID    string    `json:"room_id"`
	BookerID  string    `json:"booker_id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Attendees int       `json:"attendees"`
	Status    string    `json:"status"`
}

// toJSON normalises times to UTC, as the API contract states.
func toJSON(b store.Booking) bookingJSON {
	return bookingJSON{b.ID, b.RoomID, b.BookerID, b.Start.UTC(), b.End.UTC(), b.Attendees, b.Status}
}

func nonNil(r []booking.Room) []booking.Room {
	if r == nil {
		return []booking.Room{}
	}
	return r
}

func writeError(w http.ResponseWriter, err error) {
	var code booking.Error
	status, known := 0, errors.As(err, &code)
	if known {
		status, known = statusOf[code]
	}
	if !known {
		log.Printf("internal error: %v", err)
		code, status = "internal_error", http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]string{"code": string(code), "message": code.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
