package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poomyrd-hue/meeting-room-booking/internal/api"
	"github.com/poomyrd-hue/meeting-room-booking/internal/store"
)

func TestHandlerRouting(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := handler(api.New(s, systemClock{}))

	tests := []struct {
		name, method, path, body string
		wantCode                 int
		wantInBody               string
	}{
		{"index page", "GET", "/", "", 200, "<title>จองห้องประชุม</title>"},
		{"script", "GET", "/app.js", "", 200, "use strict"},
		{"stylesheet", "GET", "/style.css", "", 200, "--accent"},
		{"api rooms", "GET", "/rooms", "", 200, "["},
		{"api bookings reaches the API", "POST", "/bookings", "{}", 422, "invalid_request"},
		{"api cancel reaches the API", "POST", "/bookings/x/cancel", "", 422, "invalid_request"},
		{"unknown path", "GET", "/nope", "", 404, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.wantCode || !strings.Contains(rec.Body.String(), tc.wantInBody) {
				t.Fatalf("got %d %q, want %d containing %q", rec.Code, rec.Body.String(), tc.wantCode, tc.wantInBody)
			}
		})
	}
}

func TestSystemClock(t *testing.T) {
	if d := time.Since(systemClock{}.Now()); d < 0 || d > time.Minute {
		t.Fatalf("system clock off by %v", d)
	}
}
