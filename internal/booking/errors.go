package booking

// Error is a stable machine-readable code returned to API clients.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrInvalidSlot          Error = "invalid_slot"
	ErrInvalidDuration      Error = "invalid_duration"
	ErrOutsideBusinessHours Error = "outside_business_hours"
	ErrInThePast            Error = "in_the_past"
	ErrTooFarAhead          Error = "too_far_ahead"
	ErrOverCapacity         Error = "over_capacity"
	ErrTimeConflict         Error = "time_conflict"
	ErrAlreadyCancelled     Error = "already_cancelled"
	ErrAlreadyStarted       Error = "already_started"
	ErrNotBooker            Error = "not_booker"
	ErrRoomNotFound         Error = "room_not_found"
	ErrBookingNotFound      Error = "booking_not_found"
)
