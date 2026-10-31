## 1. Provider catalog operation

- [x] 1.1 Add normalized catalog/acquisition types and the single context-aware catalog method to the existing provider boundary; wire minimal Kiwix HTTP options and update provider doubles. Verify existing lifecycle behavior compiles and no startup fetch or second provider/runtime is introduced.
- [x] 1.2 Implement bounded Atom OPDS retrieval, actual offset pagination, exact full-Turkish-Wikipedia filtering, latest-per-variant selection and honest optional/Metalink metadata mapping described in design.md. Verify a fixture based on the checked live XML returns Mini/Nopic/Maxi, excludes topical/foreign/unknown entries, preserves UUID/acquisition kind and handles missing date/size without invention.
- [x] 1.3 Add meaningful local HTTP-server tests for pagination across variants, duplicate-release ordering, empty results, malformed/truncated/oversized XML, non-success status, cancellation/timeouts and non-progress/bound exhaustion. Verify errors are controlled, no partial success leaks and catalog failures leave runtime state unchanged; tests must require no public network.

## 2. Local API and package view

- [x] 2.1 Inject the same provider into `GET /api/catalog` and implement the documented success/empty, 405 and sanitized 503 JSON contract with request cancellation and existing logging. Verify API tests check success/empty arrays, Allow header, error redaction and health/shell availability after catalog failure.
- [x] 2.2 Extend the existing React shell into the scoped Turkish Bilgi Paketleri view using current styles/tokens: three variant positions, real metadata, unknown/missing/empty states, loading, recoverable failure and retry. Verify the frontend type-check/build succeeds, browser fetches only `/api/catalog`, unmount cancels work and download/install is not offered as a working action.

## 3. Integrated validation and handoff

- [x] 3.1 Run one full validation set after implementation: `bash scripts/build-frontend.sh`, `go test -race ./...`, `go vet ./...`, `go build -o /tmp/engelsiz-catalog-linux ./cmd/engelsiz`, `GOOS=windows GOARCH=amd64 go build -o /tmp/engelsiz-catalog-windows.exe ./cmd/engelsiz`, and `openspec validate kiwix-catalog --strict`. Use the available Go toolchain and `/tmp` cache as needed; verify all commands pass, then rerun only checks affected by subsequent fixes.
- [x] 3.2 Perform one targeted browser smoke covering normal metadata, missing/empty data, catalog failure and successful retry, plus health/shell survival; use controlled responses for failure cases and one small live `/api/catalog` check when networking is available. Verify no direct Kiwix browser request or ZIM transfer, record actual results/limitations, and leave existing user files untouched.
