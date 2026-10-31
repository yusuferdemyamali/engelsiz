# Engelsiz application-foundation Resources

## Knowledge

- [Go: net/http Server, ServeMux, Shutdown](https://pkg.go.dev/net/http)
  Core HTTP reference for this branch. Use for: `api.NewMux`, `web.Handler`, graceful shutdown in `internal/app/app.go`.
- [Go: database/sql Package](https://pkg.go.dev/database/sql)
  DB lifecycle reference. Use for: `database.Open`, connection handling, migration transactions.
- [Go: embed Package](https://pkg.go.dev/embed)
  Embedding reference. Use for: `//go:embed migrations/*.sql` and `//go:embed all:dist` patterns.
- [modernc.org/sqlite Driver](https://pkg.go.dev/modernc.org/sqlite)
  Pure-Go SQLite driver used here. Use for: DSN `file:...?cache=shared`, WAL / foreign_keys pragmas.
- [Vite: Static Deploy + SPA Fallback](https://vite.dev/guide/static-deploy.html)
  Frontend build reference. Use for: why `frontend/dist` + `serveIndex` fallback exists.

## Wisdom (Communities)

- [Gophers Slack #general](https://gophers.slack.com/)
  High-signal Go practitioners. Use for: idiomatic `Run(ctx)` / shutdown review.
- [r/golang](https://www.reddit.com/r/golang/)
  Moderated Go discussion. Use for: SQLite + embed patterns critique.

## Gaps

- No trusted Turkish Go + SQLite walkthrough found yet for this exact stack.
