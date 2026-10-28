# Change: application-foundation

## Why

Engelsiz v0.1 için Go core, React/TypeScript/Vite ve SQLite kullanan güvenilir bir localhost temeli gerekir. Mevcut iskeletin eksiklerini tamamlamak ve davranışını doğrulamak, sonraki Kiwix change'lerine çalışır bir app shell sağlar.

## What Changes

- `cmd/engelsiz`, `internal/{app,api,config,database,web}`, `frontend/`, `migrations/` temelini tamamla.
- Sıralı startup/shutdown, yalnızca `127.0.0.1`, tercih edilen `17321` portu ve dolu portta loopback fallback sağla.
- `GET /api/health` başarılı olduktan sonra browser'ı gerçek URL ile aç; `--no-browser` headless kullanımını koru.
- React/TypeScript/Vite, Tailwind ve shadcn/ui temel entegrasyonunu hazırla; production frontend'i Go binary'sine göm.
- Linux XDG ve Windows user app-data dizinlerini, SQLite bağlantısını ve metadata migration'larını hazırla.
- Startup/shutdown, seçilen port ve DB/migration hatalarını dosyaya kaydet.

## Capabilities

### New Capabilities

- `app-shell`: Gömülü localhost arayüzü, loopback/port seçimi, readiness sonrası browser açılışı, OS dizinleri, metadata migration'ları ve temiz lifecycle.

### Modified Capabilities

Yok; `openspec/specs/` altında mevcut bir capability bulunmuyor.

## Impact

Go entrypoint/core paketleri, frontend build girdileri, migration SQL dosyaları ve yerel build scripti etkilenir. Mevcut `go.mod` minimumu **Go 1.26.0** korunur; `modernc.org/sqlite` ile cgo'suz binary üretilir. Kullanıcı makinesinde Go, Node.js veya ayrı SQLite kurulumu gerekmez. Bu change'in API sözleşmesi `GET /api/health` ile sınırlıdır.

## Non-Goals

Kiwix catalog, `kiwix-serve`, ZIM, download manager, Wikipedia UI, search, update ve packaging kapsam dışıdır. GitHub Actions/release pipeline change 12'ye aittir. Provider/plugin sistemi veya gelecekteki modüller eklenmez; `settings` ve `providers` metadata tabloları yeterlidir, Kiwix seed'i eklenmez.

## Acceptance Criteria

1. Frontend build'i gömülerek tek executable üretilir; kaynak/build dizini ve Node/Go runtime'ı olmadan `/` ve asset'ler sunulur.
2. Server yalnızca `127.0.0.1` üzerinde çalışır. `17321` doluyken başka loopback portu seçilir ve browser gerçek URL ile açılır.
3. Health `200 {"status":"ok"}` döner; başarılı readiness kontrolünden önce browser açılmaz. `--no-browser` server/health akışını korur.
4. Linux XDG config/data/cache ayrımı ve Windows `%APPDATA%/Engelsiz` (config/logs), `%LOCALAPPDATA%/Engelsiz` (DB/content/downloads/runtime/cache) kullanılır. Executable yanına veri yazılmaz.
5. Migration metadata tablolarını oluşturur ve yeniden açılışta tekrar uygulanmaz. Bozuk DB/migration hatası açıkça kaydedilir; mevcut veriler otomatik silinmez/resetlenmez.
6. SIGINT/SIGTERM veya uygulama iptalinde HTTP ve SQLite temiz kapanır; port serbest kalır, yeniden açılışta DB kullanılabilir olur.
7. Startup/shutdown, seçilen adres ve DB hataları `logs/engelsiz.log` içinde bulunur; log dosyası açılamıyorsa açık hata bildirilir.
8. Foundation dışında business logic veya gereksiz provider/plugin soyutlamaları bulunmaz.

## Test Strategy

- Go 1.26.0+ ile `go test ./...`, `go vet ./...`, `gofmt -l`: tercih/fallback portu, loopback bind, health, browser/readiness sırası, shutdown, XDG/Windows dizinleri, migration/idempotency ve bozuk DB koruması.
- Kilit dosyasıyla frontend kurulumu, `npm run build` (TypeScript dahil), yerel embed scripti ve cgo kapalı Linux/Windows executable build'i.
- Bütünleşik smoke: temiz geçici profil, aynı origin'de health/index/asset, kaynak dizininden bağımsız ve internetsiz çalışma, dolu port, browser açıcı test dublörüyle URL/sıra, sinyal sonrası yeniden açılış. Komut/çıktı kanıtı kaydedilir; atlanan testler başarı sayılmaz.
- Windows dizinleri fake-env testleriyle ve Windows build ile kontrol edilir; Windows üzerinde çalıştırılmayan senaryolar açıkça belirtilir. Packaging/release doğrulaması yapılmaz.

## References

- `docs/main-spec.md` §§4–7, 16–18, 20–22, 26–27; AC-03/04/15/18'in foundation alt kümesi.
- `docs/changes/01-uygulama-temeli-ve-localhost-shell.md`.
