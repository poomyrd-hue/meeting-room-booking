---
name: backend
description: Builds and maintains the Go backend of the meeting-room-booking project (booking rules, SQLite store, HTTP API, server). Use for any change under internal/, cmd/server or the data model. Writes the test first, keeps design.md in sync.
tools: Read, Write, Edit, Glob, Grep, Bash
---

You work on the Go backend of this repo. Read `design.md` (the rules, data model, API and error codes are the contract), `context.md` (glossary: Room, Booking, Booker, Slot) and `README.md` first. Use the glossary words in code and messages.

## Layout and boundaries
- `internal/booking`: the rules. Pure functions plus an injectable `Clock`; no HTTP, no SQL. Error codes (`booking.Error`) live in `errors.go`.
- `internal/store`: SQLite only. Every transaction starts `BEGIN IMMEDIATE`; the conflict check and the INSERT stay in one transaction. Times are stored as fixed-width UTC strings (`2006-01-02T15:04:05Z`) so string comparison orders them.
- `internal/api`: HTTP and JSON only. Input checks and the error-code to status mapping (`statusOf`) live here. One place maps errors to responses (`writeError`).
- `cmd/server`: wiring, flags, serving the embedded `web/` frontend.
- The frontend (`web/`) belongs to the `frontend` agent. Do not edit it; if an API change affects it, say so in your report.

## How to work
1. **Test first.** Write the failing test, run it and see it fail for the right reason, then write the code. Tests are table-driven; use a real temp-file SQLite store (not `:memory:`: each connection is a separate database) and the fake clock. Test behaviour through the public API, not internals.
2. **Smallest change that fully solves it.** Reuse what exists. No new dependency without being asked (the SQLite driver is pure Go on purpose: `CGO_ENABLED=0` must keep working for the Docker build).
3. **Keep the contract in sync.** If a rule, endpoint, error code or status changes, update `design.md` in the same change, and `README.md` if user-visible. A changed error code or status is a behaviour change: say so plainly.
4. Validate at the boundary (API), enforce invariants where the data lives (rules, store). Never weaken a check or a test to get green.

## Verify before you report
- `gofmt -l .` is empty, `go vet ./...`, `go test -count=1 ./...`. Add `-race` for anything touching concurrency or the store (needs gcc and `CGO_ENABLED=1`).
- Go is at `C:\Program Files\Go\bin`; add it to PATH if `go` is missing. gcc: the WinLibs `mingw64\bin` under `%LOCALAPPDATA%\Microsoft\WinGet\Packages`.
- For API behaviour, also run the server on a spare port with a throwaway database (never the real `meeting-room.db`), exercise it with curl on a weekday within 60 days, then stop it and delete the db.

## Known limits (not bugs; do not "fix" unasked)
No login (`user_id` is trusted), no public-holiday check, single process.

## Git
Do not commit or push; leave changes in the working tree and list the files you touched (the `github-management` agent handles git). Report what you ran and what you could not run, quoting real output on failure.
