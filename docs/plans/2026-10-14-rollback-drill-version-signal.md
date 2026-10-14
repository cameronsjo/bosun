# Rollback drill version signal (issue 691)

## Checklist

- [x] Probe the pinned drill image and running incumbent before the NAS compose backup/edit.
- [x] Preserve the existing terminal, operator-prompt, and provenance constraints.
- [x] Fail transiently on a failed or unreadable drill-image probe; display an unknown incumbent without bypassing review.
- [x] Document manual rebuild guidance without adding an automatic rebuild or live-NAS behavior.
- [x] Add hermetic PTY coverage for same, drift, multiline, cold-image stderr, unknown, failures, unsafe output, ordering, and restoration.

## Verification

- `shellcheck scripts/rollback-drill.sh scripts/rollback-drill_test.sh scripts/upgrade-bosun_test.sh` — passed.
- `bash scripts/rollback-drill_test.sh` — 14 passed.
- `bash scripts/upgrade-bosun_test.sh` — 73 integrated upgrade cases plus 14 rollback-drill cases passed.
- Fresh fix-round compiler-heavy verification was blocked before compilation by the agent gate: exit 75, 25.3 GiB free versus the 100 GiB floor; no bypass was used.
- Codex independently ran the updated harness against committed pre-fix script `08fd5a4` in an isolated temporary directory. It failed at `cold-image-stderr-preamble` because the old probe returned 75 (`unreadable`) instead of proceeding to the simulated rollback. The corrected script passes that same case and the full 14-case suite.

## Review fix round 1

- [x] Merge fresh main into the published branch without rebasing.
- [x] Capture only stdout in all three version probes, leaving remote Docker commands and privilege flags unchanged.
- [x] Exercise actual stderr preambles in the fake SSH transport rather than suppressing them in the fixture.
- [x] Preserve fail-closed malformed-stdout/nonzero-probe behavior and generic error presentation.
- [x] Independently review code/security and run the 73+14 integrated shell suite.

The stdout-only correction intentionally permits a valid version alongside
benign or unsafe stderr, without displaying that stderr. It does not permit
unsafe stdout or ignore a failed probe. Final-head CI evidence for this round
belongs in the orchestrator's requested `report-fix1.md`.
