# Tasks: ระบบจองห้องประชุม

ลำดับแบบ test-first: เขียนเทสต์ที่ล้มก่อน แล้วค่อยเขียนโค้ดให้ผ่าน อ้างอิง [design.md](./design.md)

สถานะ: `[ ]` ยังไม่ทำ

## 0. เตรียมเครื่อง
- [x] ติดตั้ง Go toolchain และตรวจ `go version`
- [x] `go mod init` ใน `meeting-room/`; เพิ่ม SQLite driver แบบ pure Go (`modernc.org/sqlite`) เพื่อไม่ต้องใช้ cgo บน Windows

## 1. กฎเวลา (`internal/booking`, ฟังก์ชันบริสุทธิ์ + `Clock`)
- [x] เทสต์: slot ลงที่ :00/:30 → `invalid_slot`
- [x] เทสต์: ระยะเวลา <30 นาที หรือ >4 ชม. → `invalid_duration`
- [x] เทสต์: นอก 08:00–20:00, เสาร์–อาทิตย์, ข้ามวัน → `outside_business_hours`
- [x] เทสต์: `start` ในอดีต → `in_the_past`; เกิน 60 วัน → `too_far_ahead` (ขอบ: พอดี 60 วัน)
- [x] เทสต์: `attendees` <1 หรือ > ความจุ → `over_capacity`
- [x] เขียน `Validate(req, room, clock)` ให้ผ่านทุกเทสต์

## 2. Store (`internal/store`, SQLite in-memory ในเทสต์)
- [x] schema + migration (rooms, bookings, index)
- [x] seed ห้องจาก `rooms.json` แบบ upsert
- [x] เทสต์: ค้นหาการชน — ทับกัน/จบ=เริ่ม/ต่างห้อง/`cancelled` ไม่นับ
- [x] `CreateBooking` ใน `BEGIN IMMEDIATE` (ตรวจชน + insert) → `time_conflict`
- [x] เทสต์ concurrency: N goroutine จองห้อง/ช่วงเดียวกัน สำเร็จเพียงหนึ่ง
- [x] `Cancel`: เฉพาะ Booker (`not_booker`), `already_cancelled`, `already_started`
- [x] query: ห้องทั้งหมด, Booking รายวันของห้อง (ตามวัน Asia/Bangkok), ห้องว่างตามช่วง+จำนวนคน

## 3. API (`internal/api`, `httptest`)
- [x] เทสต์ + handler: `GET /rooms`
- [x] เทสต์ + handler: `GET /rooms/available`
- [x] เทสต์ + handler: `GET /rooms/{id}/bookings`
- [x] เทสต์ + handler: `POST /bookings` (201; 404/409/422 พร้อม `code`)
- [x] เทสต์ + handler: `POST /bookings/{id}/cancel` (200; 403/404/409)
- [x] ตัวแปลง error → HTTP status + JSON `{code, message}` ที่เดียว

## 4. ประกอบและปิดงาน
- [x] `cmd/server/main.go`: โหลด seed, เปิดฐานข้อมูล, เริ่ม server
- [x] `go vet ./...` และ `go test ./... -race` ผ่านทั้งหมด
- [x] ลองรันจริง: จอง → จองซ้อน (ต้อง 409) → ยกเลิก → จองใหม่ได้
- [x] ตรวจ `design.md` กับพฤติกรรมจริง แก้เอกสารถ้าต่าง

## 5. แก้ไข/ย้ายเวลา Booking (`PATCH /bookings/{id}`)
- [x] เทสต์ store: ย้ายเข้าช่องว่าง, ขยาย/เลื่อนทับช่วงเดิมของตัวเอง, ชนกับ Booking อื่น, ไม่นับ `cancelled`, `not_booker`, `already_cancelled`, `already_started`, ไม่พบ, แก้พร้อมกันไม่จองซ้อน
- [x] `Store.Edit` ใน `BEGIN IMMEDIATE` เดียว ใช้ `booking.Validate` ซ้ำ ไม่เขียนกฎใหม่
- [x] เทสต์ + handler API: 200 รูปแบบเดียวกับ POST, ทุก code/status, body ว่าง, ฟิลด์ไม่รู้จัก, แก้บางฟิลด์, เกินความจุ
- [x] ปรับ design.md, context.md, README.md
- [x] UI (`web/`): ปุ่ม "แก้ไข" ในตารางห้อง เปิดฟอร์มเปลี่ยนเวลาเริ่ม/ระยะเวลา/จำนวนคน

## เฟสหลัง (นอก MVP)
ล็อกอิน/สิทธิ์, UI, วันหยุดนักขัตฤกษ์, จองซ้ำ, อนุมัติ, แจ้งเตือน, API จัดการ Room
