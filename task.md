# Tasks: ระบบจองห้องประชุม

ลำดับแบบ test-first: เขียนเทสต์ที่ล้มก่อน แล้วค่อยเขียนโค้ดให้ผ่าน อ้างอิง [design.md](./design.md)

สถานะ: `[ ]` ยังไม่ทำ

## 0. เตรียมเครื่อง
- [ ] ติดตั้ง Go toolchain และตรวจ `go version`
- [ ] `go mod init` ใน `meeting-room/`; เพิ่ม SQLite driver แบบ pure Go (`modernc.org/sqlite`) เพื่อไม่ต้องใช้ cgo บน Windows

## 1. กฎเวลา (`internal/booking`, ฟังก์ชันบริสุทธิ์ + `Clock`)
- [ ] เทสต์: slot ลงที่ :00/:30 → `invalid_slot`
- [ ] เทสต์: ระยะเวลา <30 นาที หรือ >4 ชม. → `invalid_duration`
- [ ] เทสต์: นอก 08:00–20:00, เสาร์–อาทิตย์, ข้ามวัน → `outside_business_hours`
- [ ] เทสต์: `start` ในอดีต → `in_the_past`; เกิน 60 วัน → `too_far_ahead` (ขอบ: พอดี 60 วัน)
- [ ] เทสต์: `attendees` <1 หรือ > ความจุ → `over_capacity`
- [ ] เขียน `Validate(req, room, clock)` ให้ผ่านทุกเทสต์

## 2. Store (`internal/store`, SQLite in-memory ในเทสต์)
- [ ] schema + migration (rooms, bookings, index)
- [ ] seed ห้องจาก `rooms.json` แบบ upsert
- [ ] เทสต์: ค้นหาการชน — ทับกัน/จบ=เริ่ม/ต่างห้อง/`cancelled` ไม่นับ
- [ ] `CreateBooking` ใน `BEGIN IMMEDIATE` (ตรวจชน + insert) → `time_conflict`
- [ ] เทสต์ concurrency: N goroutine จองห้อง/ช่วงเดียวกัน สำเร็จเพียงหนึ่ง
- [ ] `Cancel`: เฉพาะ Booker (`not_booker`), `already_cancelled`, `already_started`
- [ ] query: ห้องทั้งหมด, Booking รายวันของห้อง (ตามวัน Asia/Bangkok), ห้องว่างตามช่วง+จำนวนคน

## 3. API (`internal/api`, `httptest`)
- [ ] เทสต์ + handler: `GET /rooms`
- [ ] เทสต์ + handler: `GET /rooms/available`
- [ ] เทสต์ + handler: `GET /rooms/{id}/bookings`
- [ ] เทสต์ + handler: `POST /bookings` (201; 404/409/422 พร้อม `code`)
- [ ] เทสต์ + handler: `POST /bookings/{id}/cancel` (200; 403/404/409)
- [ ] ตัวแปลง error → HTTP status + JSON `{code, message}` ที่เดียว

## 4. ประกอบและปิดงาน
- [ ] `cmd/server/main.go`: โหลด seed, เปิดฐานข้อมูล, เริ่ม server
- [ ] `go vet ./...` และ `go test ./... -race` ผ่านทั้งหมด
- [ ] ลองรันจริง: จอง → จองซ้อน (ต้อง 409) → ยกเลิก → จองใหม่ได้
- [ ] ตรวจ `design.md` กับพฤติกรรมจริง แก้เอกสารถ้าต่าง

## เฟสหลัง (นอก MVP)
ล็อกอิน/สิทธิ์, UI, วันหยุดนักขัตฤกษ์, จองซ้ำ, อนุมัติ, แจ้งเตือน, แก้ไข Booking, API จัดการ Room
