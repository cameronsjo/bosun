# Rollback drill version signal (issue 691)

## Checklist

- [x] Probe the pinned drill image and running incumbent before the NAS compose backup/edit.
- [x] Preserve the existing terminal, operator-prompt, and provenance constraints.
- [x] Fail transiently on a failed or unreadable drill-image probe; display an unknown incumbent without bypassing review.
- [x] Document manual rebuild guidance without adding an automatic rebuild or live-NAS behavior.
- [x] Add hermetic PTY coverage for same, drift, multiline, unknown, failures, unsafe output, ordering, and restoration.

## Verification

- `shellcheck scripts/rollback-drill.sh scripts/rollback-drill_test.sh scripts/upgrade-bosun_test.sh` — passed.
- `bash scripts/rollback-drill_test.sh` — 13 passed.
- `bash scripts/upgrade-bosun_test.sh` — 73 integrated upgrade cases plus 13 rollback-drill cases passed.
- Compiler-heavy Go verification was blocked by the agent gate: 36.6 GiB free is below the 100 GiB floor; no bypass was used.
