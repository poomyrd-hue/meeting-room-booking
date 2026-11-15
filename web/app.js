"use strict";

const $ = (id) => document.getElementById(id);
const OPEN = 8 * 60, CLOSE = 20 * 60, STEP = 30, MAX_MIN = 240, MAX_DAYS = 60;

const ERRORS = {
  invalid_slot: "เวลาต้องเริ่มและจบที่ :00 หรือ :30",
  invalid_duration: "ระยะเวลาต้องอยู่ระหว่าง 30 นาที ถึง 4 ชั่วโมง",
  outside_business_hours: "จองได้เฉพาะ 08:00–20:00 วันจันทร์–ศุกร์",
  in_the_past: "เวลานี้ผ่านไปแล้ว",
  too_far_ahead: "จองล่วงหน้าได้ไม่เกิน 60 วัน",
  over_capacity: "จำนวนคนเกินความจุของห้อง",
  time_conflict: "ห้องนี้ถูกจองในช่วงเวลานี้แล้ว กรุณาเลือกเวลาหรือห้องอื่น",
  already_cancelled: "การจองนี้ถูกยกเลิกไปแล้ว",
  already_started: "การจองนี้เริ่มไปแล้ว ยกเลิกไม่ได้",
  not_booker: "ยกเลิกได้เฉพาะผู้จองเท่านั้น",
  room_not_found: "ไม่พบห้องนี้",
  booking_not_found: "ไม่พบการจองนี้",
  invalid_request: "ข้อมูลไม่ครบหรือไม่ถูกต้อง",
  network: "เชื่อมต่อเซิร์ฟเวอร์ไม่ได้ กรุณาลองใหม่",
};
const errText = (code) => ERRORS[code] || "เกิดข้อผิดพลาดที่ไม่คาดคิด กรุณาลองใหม่";

let rooms = [];

// ---- helpers -------------------------------------------------------------
async function api(path, opts) {
  let res;
  try { res = await fetch(path, opts); } catch { throw { code: "network" }; }
  const body = await res.json().catch(() => null);
  if (!res.ok) throw { code: (body && body.code) || "internal_error" };
  return body;
}

// Bangkok wall-clock "now" as a Date whose UTC fields hold Bangkok time (UTC+7, no DST).
const bkkNow = () => new Date(Date.now() + 7 * 3600e3);
const bkkDate = (d = bkkNow()) => d.toISOString().slice(0, 10);
const pad = (n) => String(n).padStart(2, "0");
const hhmm = (min) => `${pad(Math.floor(min / 60))}:${pad(min % 60)}`;
const weekday = (date) => new Date(date + "T00:00:00Z").getUTCDay(); // 0 = Sunday
const fmtTime = (iso) => new Date(iso).toLocaleTimeString("th-TH", { timeZone: "Asia/Bangkok", hour: "2-digit", minute: "2-digit", hour12: false });
const fmtDate = (date) => new Date(date + "T00:00:00Z").toLocaleDateString("th-TH", { timeZone: "UTC", weekday: "long", day: "numeric", month: "long", year: "numeric" });
const bkkShift = (iso) => new Date(new Date(iso).getTime() + 7 * 3600e3);
const bkkMinutes = (iso) => bkkShift(iso).getUTCHours() * 60 + bkkShift(iso).getUTCMinutes();
const rfc = (date, min) => `${date}T${hhmm(min)}:00+07:00`;

function el(tag, props = {}, ...kids) {
  const e = Object.assign(document.createElement(tag), props);
  e.append(...kids);
  return e;
}

function say(text, kind) {
  const s = $("status");
  s.hidden = !text;
  s.textContent = text || "";
  s.className = "status " + (kind || "");
  if (text) s.dataset.icon = kind === "err" ? "⚠" : "✓"; // icon so colour is not the only signal
  else delete s.dataset.icon;
}

async function busy(btn, fn) {
  btn.disabled = true;
  try { await fn(); } finally { btn.disabled = false; }
}

// ---- search form ---------------------------------------------------------
const durLabel = (d) => (d % 60 === 0 ? `${d / 60} ชั่วโมง` : d > 60 ? `${Math.floor(d / 60)} ชั่วโมง 30 นาที` : "30 นาที");

// Start minutes still bookable on a Bangkok date (later than now when the date is today).
function startMinutes(date) {
  const now = bkkNow(), nowMin = date === bkkDate() ? now.getUTCHours() * 60 + now.getUTCMinutes() : -1;
  const out = [];
  for (let m = OPEN; m < CLOSE; m += STEP) if (m > nowMin) out.push(m);
  return out;
}

function durationMinutes(start) {
  const out = [];
  for (let d = STEP; d <= MAX_MIN && start + d <= CLOSE; d += STEP) out.push(d);
  return out;
}

function fillSelect(sel, values, label, prev) {
  sel.replaceChildren(...values.map((v) => el("option", { value: v, textContent: label(v) })));
  if (values.includes(prev)) sel.value = prev;
}

function fillStarts() {
  fillSelect($("start"), startMinutes($("date").value), hhmm, Number($("start").value));
  fillDurations();
}

function fillDurations() {
  fillSelect($("duration"), durationMinutes(Number($("start").value)), durLabel, Number($("duration").value));
}

// Today if a slot is still left and it is a weekday, otherwise the next weekday.
function firstBookableDate() {
  const now = bkkNow();
  let d = new Date(now), late = now.getUTCHours() * 60 + now.getUTCMinutes() >= CLOSE - STEP;
  if (late) d = new Date(d.getTime() + 864e5);
  while ([0, 6].includes(d.getUTCDay())) d = new Date(d.getTime() + 864e5);
  return bkkDate(d);
}

function currentQuery() {
  const date = $("date").value, start = Number($("start").value), dur = Number($("duration").value);
  const n = Number($("attendees").value);
  if (!date || !start || !dur) return null;
  return { date, start, end: start + dur, attendees: n };
}

async function search() {
  const q = currentQuery();
  const day = q && weekday(q.date);
  if (!q || !(q.attendees >= 1)) return say(errText("invalid_request"), "err");
  if (day === 0 || day === 6) return say(errText("outside_business_hours"), "err");
  const params = new URLSearchParams({ start: rfc(q.date, q.start), end: rfc(q.date, q.end), attendees: q.attendees });
  try {
    const list = await api("/rooms/available?" + params);
    renderResults(list, q);
  } catch (e) { say(errText(e.code), "err"); }
}

function renderResults(list, q) {
  const ul = $("results");
  if (!list.length) return ul.replaceChildren(el("li", { className: "empty" }, "ไม่มีห้องว่างที่จุคนได้ในช่วงเวลานี้"));
  ul.replaceChildren(...list.map((r) => {
    const btn = el("button", { type: "button", textContent: "จองห้องนี้" });
    btn.setAttribute("aria-label", `จองห้อง ${r.name}`);
    btn.onclick = () => busy(btn, () => book(r, q));
    return el("li", {}, el("div", {}, el("strong", {}, r.name), el("div", { className: "meta" }, `ชั้น ${r.floor} · จุ ${r.capacity} คน`)), btn);
  }));
}

async function book(room, q) {
  const user = $("user").value.trim();
  if (!user) { say("กรุณากรอกชื่อของคุณก่อนจอง", "err"); $("user").focus(); return; }
  try {
    await api("/bookings", { method: "POST", body: JSON.stringify({ room_id: room.id, user_id: user, start: rfc(q.date, q.start), end: rfc(q.date, q.end), attendees: q.attendees }) });
    $("room").value = room.id; $("sdate").value = q.date; // show the new booking in the schedule
    say(`จองห้อง ${room.name} สำเร็จ · ${fmtDate(q.date)} ${hhmm(q.start)}–${hhmm(q.end)}`, "ok");
  } catch (e) { say(errText(e.code), "err"); }
  await search(); // availability changed either way
  await loadSchedule();
}

// ---- schedule ------------------------------------------------------------
async function loadSchedule() {
  const id = $("room").value, date = $("sdate").value, ul = $("schedule");
  if (!id || !date) return;
  try {
    const list = await api(`/rooms/${encodeURIComponent(id)}/bookings?date=${date}`);
    if (!list.length) return ul.replaceChildren(el("li", { className: "empty" }, "ยังไม่มีการจองในวันนี้"));
    const user = $("user").value.trim(), now = Date.now();
    ul.replaceChildren(...list.map((b) => {
      const mine = user && b.booker_id === user, started = new Date(b.start).getTime() <= now;
      const right = el("span", { className: "meta actions" });
      const li = el("li", {}, el("div", {}, el("strong", {}, `${fmtTime(b.start)}–${fmtTime(b.end)}`), el("div", { className: "meta" }, `ผู้จอง ${b.booker_id} · ${b.attendees} คน${mine ? " (คุณ)" : ""}`)), right);
      if (mine && !started) {
        const range = `${fmtTime(b.start)}–${fmtTime(b.end)}`;
        const edit = el("button", { type: "button", className: "secondary", textContent: "แก้ไข" });
        edit.setAttribute("aria-label", `แก้ไขการจอง ${range}`);
        edit.onclick = () => { const f = editForm(b); li.replaceChildren(f); f.querySelector("select").focus(); };
        const btn = el("button", { type: "button", className: "secondary", textContent: "ยกเลิก" });
        btn.setAttribute("aria-label", `ยกเลิกการจอง ${range}`);
        btn.onclick = () => busy(btn, () => cancel(b));
        right.replaceChildren(edit, btn);
      } else if (started) right.textContent = "เริ่มแล้ว";
      return li;
    }));
  } catch (e) { say(errText(e.code), "err"); }
}

function editForm(b) {
  const date = bkkDate(bkkShift(b.start)), room = rooms.find((r) => r.id === b.room_id);
  const startMin = bkkMinutes(b.start), durMin = (new Date(b.end) - new Date(b.start)) / 60000;
  const start = el("select"), dur = el("select");
  const n = el("input", { type: "number", min: 1, max: room ? room.capacity : 100, value: b.attendees, required: true });
  fillSelect(start, startMinutes(date), hhmm, startMin);
  fillSelect(dur, durationMinutes(Number(start.value)), durLabel, durMin);
  start.onchange = () => fillSelect(dur, durationMinutes(Number(start.value)), durLabel, Number(dur.value));
  const save = el("button", { type: "submit", textContent: "บันทึก" });
  const close = el("button", { type: "button", className: "secondary", textContent: "ไม่แก้ไข" });
  close.onclick = () => loadSchedule();
  const field = (text, ctl) => el("label", {}, text, ctl);
  const form = el("form", { className: "edit" }, field("เวลาเริ่ม", start), field("ระยะเวลา", dur), field("จำนวนคน", n), save, close);
  form.onsubmit = (ev) => { ev.preventDefault(); busy(save, () => saveEdit(b, date, Number(start.value), Number(dur.value), Number(n.value))); };
  return form;
}

async function saveEdit(b, date, start, dur, attendees) {
  const patch = {};
  if (start !== bkkMinutes(b.start) || start + dur !== bkkMinutes(b.end)) { patch.start = rfc(date, start); patch.end = rfc(date, start + dur); }
  if (attendees !== b.attendees) patch.attendees = attendees;
  if (!Object.keys(patch).length) return loadSchedule(); // nothing changed
  try {
    await api(`/bookings/${b.id}`, { method: "PATCH", headers: { "X-User-Id": $("user").value.trim() }, body: JSON.stringify(patch) });
  } catch (e) { say(errText(e.code), "err"); return; } // keep the form open so the input is not lost
  say("แก้ไขการจองแล้ว", "ok");
  await loadSchedule();
  if (currentQuery()) await search();
}

async function cancel(b) {
  if (!confirm(`ยกเลิกการจอง ${fmtTime(b.start)}–${fmtTime(b.end)} ใช่หรือไม่?`)) return;
  try {
    await api(`/bookings/${b.id}/cancel`, { method: "POST", headers: { "X-User-Id": $("user").value.trim() } });
    say("ยกเลิกการจองแล้ว", "ok");
  } catch (e) { say(errText(e.code), "err"); }
  await loadSchedule();
  if (currentQuery()) await search();
}

// ---- init ----------------------------------------------------------------
async function init() {
  try { $("user").value = localStorage.getItem("user") || ""; } catch {}
  $("user").addEventListener("change", () => {
    try { localStorage.setItem("user", $("user").value.trim()); } catch {}
    loadSchedule();
  });

  const today = bkkDate(), first = firstBookableDate(), max = bkkDate(new Date(bkkNow().getTime() + MAX_DAYS * 864e5));
  for (const id of ["date", "sdate"]) Object.assign($(id), { value: first, min: id === "date" ? today : "", max: id === "date" ? max : "" });
  fillStarts();

  $("date").onchange = fillStarts;
  $("start").onchange = fillDurations;
  $("search").onsubmit = (ev) => { ev.preventDefault(); say(""); busy(ev.submitter, search); };
  $("room").onchange = $("sdate").onchange = loadSchedule;

  try {
    rooms = await api("/rooms");
    $("room").replaceChildren(...rooms.map((r) => el("option", { value: r.id, textContent: `${r.name} (ชั้น ${r.floor}, ${r.capacity} คน)` })));
    await loadSchedule();
  } catch (e) { say(errText(e.code), "err"); }
}
init();
