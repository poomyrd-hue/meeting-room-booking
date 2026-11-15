# Meeting Room Booking

REST API สำหรับจองห้องประชุมภายในองค์กรเดียว เขียนด้วย Go + SQLite

- จอง/ยกเลิกห้อง โดยห้ามจองซ้อนเวลาในห้องเดียวกัน
- ดูตารางของห้องรายวัน และค้นหาห้องว่างที่จุคนได้
- ไม่มี UI และไม่มีล็อกอินใน MVP (ดู [ข้อจำกัด](#ข้อจำกัด))

เอกสารออกแบบ: [context.md](context.md) (ศัพท์และขอบเขต), [design.md](design.md) (กฎ, API, data model), [task.md](task.md) (งานที่ทำ)

## เริ่มใช้งาน

ต้องมี Go 1.27+ (ไม่ต้องมี C compiler)

```bash
go run ./cmd/server
```

| Flag | ค่าเริ่มต้น | ความหมาย |
|---|---|---|
| `-addr` | `:8080` | ที่อยู่ที่ฟัง |
| `-db` | `meeting-room.db` | ไฟล์ SQLite (สร้างให้เองถ้ายังไม่มี) |
| `-rooms` | `rooms.json` | ไฟล์ห้อง โหลดทุกครั้งที่เริ่ม (upsert ตาม `id`) |

เพิ่มหรือแก้ห้องโดยแก้ [rooms.json](rooms.json) แล้วเริ่มเซิร์ฟเวอร์ใหม่:

```json
[{"id": "r1", "name": "Alpha", "capacity": 8, "floor": 1}]
```

## กฎการจอง

- ช่วงละ 30 นาที ขั้นต่ำ 30 นาที สูงสุด 4 ชั่วโมง เริ่ม/จบที่ :00 หรือ :30
- จองได้ 08:00–20:00 (Asia/Bangkok) จันทร์–ศุกร์ ภายในวันเดียว
- จองย้อนหลังไม่ได้ และล่วงหน้าได้ไม่เกิน 60 วัน
- จำนวนผู้เข้าร่วมต้องไม่เกินความจุห้อง
- จบ 10:00 กับเริ่ม 10:00 ในห้องเดียวกันไม่ถือว่าชน
- ยกเลิกได้เฉพาะผู้จอง และก่อนเวลาเริ่มเท่านั้น ไม่มีการแก้ไข (ยกเลิกแล้วจองใหม่)

## API

เวลาเป็น RFC 3339 (ส่ง offset ใดก็ได้ ตอบกลับเป็น UTC)

| Endpoint | หน้าที่ |
|---|---|
| `GET /rooms` | รายการห้อง |
| `GET /rooms/available?start=&end=&attendees=` | ห้องที่ว่างตลอดช่วงและจุคนได้ |
| `GET /rooms/{id}/bookings?date=YYYY-MM-DD` | การจองของห้องในวันนั้น (ตามเวลา Bangkok) |
| `POST /bookings` | จอง body: `room_id, user_id, start, end, attendees` → 201 |
| `POST /bookings/{id}/cancel` | ยกเลิก ส่ง header `X-User-Id` → 200 |

```bash
# จอง
curl -X POST localhost:8080/bookings -d '{
  "room_id": "r1", "user_id": "alice",
  "start": "2026-11-17T10:00:00+07:00", "end": "2026-11-17T11:00:00+07:00",
  "attendees": 4}'

# ยกเลิก
curl -X POST localhost:8080/bookings/<id>/cancel -H 'X-User-Id: alice'
```

### ข้อผิดพลาด

รูปแบบ `{"code": "...", "message": "..."}`

| HTTP | code |
|---|---|
| 422 | `invalid_slot`, `invalid_duration`, `outside_business_hours`, `in_the_past`, `too_far_ahead`, `over_capacity`, `invalid_request` |
| 409 | `time_conflict`, `already_cancelled`, `already_started` |
| 403 | `not_booker` |
| 404 | `room_not_found`, `booking_not_found` |

## พัฒนาและทดสอบ

```bash
go vet ./...
go test ./...
```

โครงสร้าง: `internal/booking` (กฎ ไม่ผูกกับ HTTP), `internal/store` (SQLite), `internal/api` (HTTP), `cmd/server` (main)

## ข้อจำกัด

- **ไม่มีล็อกอิน:** ระบบเชื่อ `user_id` ที่ผู้เรียกส่งมา ใช้หลังเครือข่ายองค์กรที่เชื่อถือได้เท่านั้น
- ไม่เช็กวันหยุดนักขัตฤกษ์
- รันโปรเซสเดียว การกันจองซ้อนอาศัย transaction ของ SQLite ถ้าจะรันหลายโปรเซสต้องย้ายฐานข้อมูล
- ยังไม่ได้ทดสอบด้วย `go test -race`
