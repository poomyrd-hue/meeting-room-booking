// Command server runs the meeting room booking API.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/poomyrd-hue/meeting-room-booking/internal/api"
	"github.com/poomyrd-hue/meeting-room-booking/internal/store"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "meeting-room.db", "SQLite database file")
	roomsPath := flag.String("rooms", "rooms.json", "rooms seed file")
	flag.Parse()

	if err := run(*addr, *dbPath, *roomsPath); err != nil {
		log.Fatal(err)
	}
}

func run(addr, dbPath, roomsPath string) error {
	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()

	rooms, err := store.LoadRooms(roomsPath)
	if err != nil {
		return err
	}
	if err := s.SeedRooms(context.Background(), rooms); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.New(s, systemClock{}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	log.Printf("listening on %s (%d rooms)", addr, len(rooms))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
