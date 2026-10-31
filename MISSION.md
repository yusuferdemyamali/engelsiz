# Mission: feat/1-application-foundation branch

## Why
Understand how Engelsiz boots end-to-end on this branch so I can extend it without guessing — config, DB, HTTP, frontend serving.

## Success looks like
- Trace `app.Run()` startup/shutdown from memory: dirs → logger → DB → listen → health → browser
- Explain why `ListenLoopback(17321)` falls back and why `waitForHealth` gates browser open
- Run `go test ./...` and `scripts/build-frontend.sh` and predict what each verifies

## Constraints
- Deep dives preferred, Turkish explanations welcome
- Learn in workspace `/home/yusuf/Belgeler/projects/engelsiz`

## Out of scope
- Features beyond application-foundation (auth, content, downloads)
- Frontend design polish
