FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# modernc.org/sqlite is pure Go, so no cgo and a static binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
COPY rooms.json /app/rooms.json
# The SQLite file lives here; mount a volume to keep data across containers.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/server", "-addr", ":8080", "-db", "/data/meeting-room.db", "-rooms", "/app/rooms.json"]
