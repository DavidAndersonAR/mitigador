---
status: partial
phase: 01-observation-spine
source: [01-VERIFICATION.md]
started: 2026-05-19T12:40:00Z
updated: 2026-05-19T12:40:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Mikrotik NetFlow v9 path end-to-end
expected: Operator configures a real Mikrotik router to export NetFlow v9 to UDP/2055; within 60s the exporter appears online in /exporters view with correct pps/bps (despite Mikrotik byte-order bug, corrected by sample_rate_override). Roadmap SC #1.
result: [pending]

### 2. Telegram alert end-to-end on real bot
expected: With a real Telegram bot token and chat ID configured, operator receives a pt-BR alert within the detection latency (<=7s from threshold crossing) containing IP, vector, pps/bps, and duration. No BGP announcement is made. Roadmap SC #2.
result: [pending]

### 3. Locale toggle PT/EN in browser
expected: Operator clicks the locale toggle in the dashboard; all labels switch from pt-BR to en-US strings defined in en-US.json without page reload.
result: [pending]

### 4. Dark theme renders correctly
expected: Dashboard loads with Naive UI dark theme by default; sidebar, tables, and status dots are visually correct against the dark background.
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
