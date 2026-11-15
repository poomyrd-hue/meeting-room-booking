// Package booking holds the booking rules. It knows nothing about HTTP or storage.
package booking

import "time"

// Bangkok is a fixed UTC+7 zone: Thailand has no DST, and this avoids needing tzdata on Windows.
var Bangkok = time.FixedZone("Asia/Bangkok", 7*60*60)

const (
	slot        = 30 * time.Minute
	maxDuration = 4 * time.Hour
	maxAhead    = 60 * 24 * time.Hour
	openMinute  = 8 * 60  // 08:00
	closeMinute = 20 * 60 // 20:00
)

type Clock interface{ Now() time.Time }

type Room struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
	Floor    int    `json:"floor"`
}

type Request struct {
	RoomID    string
	UserID    string
	Start     time.Time
	End       time.Time
	Attendees int
}

// Validate checks the time and capacity rules, in the order the API reports them.
func Validate(r Request, room Room, clock Clock) error {
	start, end := r.Start.In(Bangkok), r.End.In(Bangkok)

	if !onSlot(start) || !onSlot(end) {
		return ErrInvalidSlot
	}
	if d := end.Sub(start); d < slot || d > maxDuration {
		return ErrInvalidDuration
	}
	if !withinBusinessHours(start, end) {
		return ErrOutsideBusinessHours
	}
	now := clock.Now()
	if start.Before(now) {
		return ErrInThePast
	}
	if start.After(now.Add(maxAhead)) {
		return ErrTooFarAhead
	}
	if r.Attendees < 1 || r.Attendees > room.Capacity {
		return ErrOverCapacity
	}
	return nil
}

func onSlot(t time.Time) bool {
	return t.Second() == 0 && t.Nanosecond() == 0 && t.Minute()%int(slot/time.Minute) == 0
}

func withinBusinessHours(start, end time.Time) bool {
	if wd := start.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	y1, m1, d1 := start.Date()
	y2, m2, d2 := end.Date()
	if y1 != y2 || m1 != m2 || d1 != d2 {
		return false
	}
	return minuteOfDay(start) >= openMinute && minuteOfDay(end) <= closeMinute
}

func minuteOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }
