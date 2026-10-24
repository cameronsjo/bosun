## 1. Retract interruption alerts

- [x] 1.1 Send the recovery alert at the finalizer site that clears an `interrupted` outcome, only when the run ended without error
- [x] 1.2 Keep the marker when delivery fails; clear it when dispatched or when `on_recovery` is off
- [x] 1.3 Suppress the finalizer recovery when the run already dispatched one
- [x] 1.4 Tests: interruption then success sends one; repeated successes send none; `on_recovery=false` sends none; delivery failure retries; failed run sends none; no double send
- [x] 1.5 Failed runs keep the marker; interruption recovery text omits the failure count
