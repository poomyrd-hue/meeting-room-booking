# Design: ระบบจองห้องประชุม

ดูศัพท์และขอบเขตใน [context.md](./context.md)

## กฎของ Booking

| กฎ | ค่า |
|---|---|
| ช่วงเวลา | `[start, end)` — จบ 10:00 กับเริ่ม 10:00 ในห้องเดียวกัน **ไม่ชน** |
| ขนาด Slot | 30 นาที; `start`/`end` ต้องลงที่ :00 หรือ :30 |
| ระยะเวลา | ขั้นต่ำ 30 นาที, สูงสุด 4 ชั่วโมง |
| เวลาทำการ | 08:00–20:00 Asia/Bangkok, จันทร์–ศุกร์; Booking ต้องอยู่ในวันเดียวและในเวลาทำการทั้งช่วง |
| ย้อนหลัง | `start` ต้องไม่อยู่ในอดีต |
| ล่วงหน้า | `start` ไม่เกิน 60 วันนับจากปัจจุบัน |
| ความจุ | `attendees` ≤ `room.capacity` (`over_capacity`); `attendees` < 1 ถือเป็นข้อมูลผิด (`invalid_request`) |
| ผู้จอง | `user_id` ตัดช่องว่างหน้า-หลังแล้วต้องไม่ว่าง และยาวไม่เกิน 60 ตัวอักษร ไม่เช่นนั้น `invalid_request` (ใช้กับ `X-User-Id` ด้วย) |
| ชน | ห้องเดียวกัน, status `confirmed`, ช่วงเวลาทับกัน → ปฏิเสธ (`cancelled` ไม่นับ) |
| ยกเลิก | เฉพาะ Booker; เฉพาะ Booking ที่ `confirmed` และ `start` ยังไม่ถึง; ไม่ลบข้อมูล |
| แก้ไข | ไม่มี — ยกเลิกแล้วจองใหม่ |

## เวลา

- เก็บและส่งใน API เป็น RFC 3339 UTC; ตรวจกฎเวลาทำการ/วันทำการโดยแปลงเป็น Asia/Bangkok (UTC+7 ไม่มี DST)
- นาฬิกาเป็น dependency ที่ฉีดเข้ามา (interface `Clock`) เพื่อทดสอบกฎย้อนหลัง/ล่วงหน้า

## Data model (SQLite)

```
rooms(id TEXT PK, name TEXT, capacity INTEGER, floor INTEGER)
bookings(
  id TEXT PK,            -- uuid
  room_id TEXT FK rooms,
  booker_id TEXT,        -- user_id ที่ผู้เรียกส่งมา
  start_utc TEXT,        -- RFC 3339
  end_utc TEXT,
  attendees INTEGER,
  status TEXT CHECK (status IN ('confirmed','cancelled')),
  created_at TEXT,
  cancelled_at TEXT NULL
)
INDEX bookings(room_id, start_utc, end_utc) WHERE status='confirmed'
```

Room เพิ่มจากไฟล์ seed (`rooms.json`) ตอนเริ่มระบบ แบบ upsert ตาม `id`

## API

ทุก request ที่เปลี่ยนข้อมูลส่ง `user_id` ใน body (POST /bookings) หรือ header `X-User-Id` (cancel)

| Endpoint | หน้าที่ |
|---|---|
| `GET /rooms` | รายการห้อง |
| `GET /rooms/available?start=&end=&attendees=` | ห้องที่ว่างตลอดช่วงและจุคนได้ |
| `GET /rooms/{id}/bookings?date=YYYY-MM-DD` | Booking `confirmed` ของห้องในวันนั้น (วันตาม Asia/Bangkok) |
| `POST /bookings` | body: `room_id, user_id, start, end, attendees` → 201 |
| `POST /bookings/{id}/cancel` | ยกเลิก → 200 |

หมายเหตุพฤติกรรมจริง: `GET /rooms/available` ตรวจแค่ว่า `start`/`end` เป็น RFC 3339, `end` > `start`, `attendees` ≥ 1 (ผิด → 422 `invalid_request`) ไม่ตรวจกฎเวลาทำการ; ฟิลด์ `message` ในข้อผิดพลาดเท่ากับ `code`

### ข้อผิดพลาด

รูปแบบ: `{"code": "...", "message": "..."}`

| HTTP | code | กรณี |
|---|---|---|
| 422 | `invalid_slot`, `invalid_duration`, `outside_business_hours`, `in_the_past`, `too_far_ahead`, `over_capacity`, `invalid_request` | กฎเวลา/ความจุ/ข้อมูลผิด |
| 409 | `time_conflict` | ชนกับ Booking อื่น |
| 409 | `already_cancelled`, `already_started` | ยกเลิกไม่ได้ตามสถานะ |
| 403 | `not_booker` | ยกเลิกของคนอื่น |
| 404 | `room_not_found`, `booking_not_found` | ไม่พบ |

ลำดับตรวจ POST /bookings: ข้อมูลครบ → ห้องมีอยู่ → กฎเวลา → ความจุ → ชน (ภายใน transaction)

## การกันจองซ้อน

ตรวจชนและ INSERT ใน transaction เดียวด้วย `BEGIN IMMEDIATE` (SQLite ล็อกเขียนทั้งฐาน) รันโปรเซสเดียว ไม่มี lock ภายนอก
- shortcut: ใช้ได้เฉพาะโปรเซสเดียว ถ้าขยายหลายโปรเซส/หลายเครื่อง ให้ย้ายไป Postgres และใช้ exclusion constraint บน `tstzrange`

## โครงสร้างโค้ด (Go)

```
meeting-room/
  cmd/server/main.go     # wiring, โหลด seed
  internal/booking/      # กฎ + service (ไม่รู้จัก HTTP)
  internal/store/        # SQLite
  internal/api/          # handlers, JSON, error mapping
  rooms.json
```

กฎอยู่ใน `internal/booking` เป็นฟังก์ชันบริสุทธิ์ + `Clock` เพื่อให้ทดสอบแบบ table-driven ได้โดยไม่ต้องใช้ HTTP

## การทดสอบ

- table-driven ครอบทุกกฎในตารางข้างบน รวมขอบ: จบ=เริ่มไม่ชน, ข้ามวัน, 20:00 พอดี, 60 วันพอดี, ยกเลิกแล้วห้องว่าง
- ชั้น store ใช้ SQLite in-memory
- ทดสอบจองพร้อมกันหลาย goroutine ห้อง/ช่วงเดียวกัน ต้องสำเร็จเพียงหนึ่ง
- ชั้น API: `httptest` ตรวจรหัสสถานะและ `code`

## ความเสี่ยงที่รู้อยู่

- `user_id` ปลอมได้ (ไม่มีล็อกอิน)
- ไม่รู้วันหยุดนักขัตฤกษ์
- โปรเซสเดียว
