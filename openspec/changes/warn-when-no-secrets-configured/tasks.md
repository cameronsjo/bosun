# Tasks

- [x] 1. Move the empty-list check above `ui.Info("Decrypting secrets...")` in
  `decryptSecrets`, so a run that decrypts nothing never says it is decrypting.
- [x] 2. Raise the skip from `Debug` to `Warn` and name `BOSUN_SECRETS_FILE` in
  the message.
- [x] 3. Add the console counterpart (`ui.Warning`) for the non-JSON path.
- [x] 4. Test the warning at `WarnLevel`, with a control arm proving a
  configured run does not emit it. Staged as a break: reverting to the `Debug`
  line turns the new test red on both assertions.
- [x] 5. Spec delta: `specs/reconcile/spec.md`, MODIFIED Secret Decryption.
