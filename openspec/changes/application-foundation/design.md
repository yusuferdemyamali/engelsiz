# Design: application-foundation

## Context

Kaynaklar: `proposal.md`, `docs/main-spec.md` ve change 01 prompt'u. Go/React/SQLite iskeleti zaten mevcut; bu çalışma yeniden yazım değil, mevcut foundation'ın tamamlanmasıdır. `go.mod` minimumu Go 1.26.0'dır; doğrulama ortamında Go 1.26.10 kullanılabilir. Önceki 1.23+ ifadesi mevcut bağımlılıklar için geçerli kabul edilmez.

## Goals / Non-Goals

**Goals:** Tek executable içinde yerel UI, OS-standard veri konumları, sıralı startup ve güvenli kapanış. Mevcut küçük paket sınırlarını koru.

**Non-Goals:** Provider interface/plugin SDK, Kiwix seed/runtime, katalog/download/search/update, packaging ve GitHub Actions/release işi. Kalıcı settings API'si veya yeni config framework'ü kurulmaz; bu change'de config altyapısı dizin/env çözümlemesi ve metadata tablosudur.

## Decisions

### 1. Repo ve build girdileri

`cmd/engelsiz/main.go`, `internal/{app,api,config,database,web}`, `frontend/`, `migrations/` mevcut sınırlarıdır. Gelecekteki business paketleri açılmaz. `.gitignore` yalnızca üretilen binary/build/veri çıktılarını dışlar; entrypoint ve embed placeholder'ı temiz checkout'ta bulunmalıdır.

Frontend üretimi `frontend/dist` -> `internal/web/dist` kopyasıyla yapılır; Go embed kendi paketinin üstünden dosya alamadığı için bu küçük build adımı seçilir. `//go:embed all:dist`, henüz frontend üretilmemişken `.gitkeep` ile kaynak test derlemesine izin verir. Ancak placeholder çalışan UI sayılmaz: teslim edilen binary için production build zorunludur; `/`, gerçek `/assets/*` ve `/api/health` smoke kontrolü atlanamaz. Üretilen asset'ler commit'e girmez. Alternatif harici frontend server'ı kullanıcıya ek runtime gerektirdiğinden kullanılmaz.

### 2. Frontend temeli

React + TypeScript + Vite, Tailwind tema token'ları, `@/` alias'ı ve shadcn/ui kurulum metadata/tooling'i hazırlanır. Yalnızca foundation'ı gösteren küçük bir home bulunur; paket/search/Wikipedia sayfaları eklenmez. Yeni bileşen kütüphanesi veya genel tema/provider framework'ü gerekmez.

### 3. Startup, network ve browser

Dizin/env çözümleme -> dizin oluşturma -> log -> SQLite/migration -> listen -> self-health -> browser sırası kullanılır. Server `127.0.0.1:17321` dinler; port doluysa `127.0.0.1:0` ile kernel'den alternatif alır. Diğer listen hataları bağlamıyla raporlanır. Wildcard/IPv6 bind veya LAN modu yoktur.

Health kontrolü başarılı olmadan browser başlatılmaz; `--no-browser` yalnızca açıcıyı atlar. Windows'ta `rundll32 url.dll,FileProtocolHandler`, Linux'ta `xdg-open` argümanları shell oluşturmadan process API ile iletilir; açıcı hatası kaydedilir. macOS destek hedefi değildir. Test için dar bir açıcı dublörü veya executable fixture yeterlidir; plugin sistemi gerekmez.

SIGINT/SIGTERM veya context iptalinde HTTP kapanır, DB/log kaynakları bırakılır; startup hatalarında edinilmiş kaynaklar da temizlenir. Readiness beklerken iptal edilen uygulama sonradan browser açmaz. Tamamlanan OS açıcı process'i çalışma sırasında asenkron beklenerek reap edilir. Açıcı browser'ın kendisine dönüşebileceğinden uygulama kapanışında bu process öldürülmez ve bitmesi beklenmez; kullanıcının browser yaşam döngüsü bağımsızdır.

### 4. Dizinler

- Linux: `${XDG_CONFIG_HOME:-~/.config}/engelsiz/config`, `${XDG_DATA_HOME:-~/.local/share}/engelsiz/{engelsiz.db,logs,downloads,content,runtime}`, `${XDG_CACHE_HOME:-~/.cache}/engelsiz/cache`.
- Windows: `%APPDATA%/Engelsiz/{config,logs}`, `%LOCALAPPDATA%/Engelsiz/{engelsiz.db,downloads,content,runtime,cache}`; env yoksa kullanıcı home altındaki standart AppData kökleri.

Dizin çözümleme executable/current directory'ye düşmez; home çözülememesi açık hatadır. XDG kökleri OS standardına uygun mutlak yollardır. Alternatif tek data klasörü yerine bu ayrım §17'yi karşılar.

### 5. SQLite ve log

Mevcut `modernc.org/sqlite` cgo'suz bağımlılık olarak korunur. WAL, busy timeout ve versioned migration runner kullanılır; `schema_migrations` ile sıralı SQL dosyaları transaction içinde yalnızca bir kez uygulanır. `001_init.sql` yalnızca `settings(key,value,updated_at)` ve `providers(id,type,enabled,created_at,updated_at)` oluşturur. Root ve embedded SQL kopyaları tutarlı tutulur. Bozuk DB veya başarısız migration otomatik silme/reset yapmaz.

Temel dosya logu startup/shutdown, gerçek adres ve DB/migration hatalarını içerir. Log açılamaması sessiz başarıya dönüşmez; stderr'de açık hata verilir. Rotation sistemi veya ayrıntılı telemetry bu change'e eklenmez.

## Risks / Trade-offs

- Placeholder ile derlenen binary UI sunmayabilir -> üretim build sırası ve atlanmayan binary/asset smoke kontrolü.
- Headless ortamda gerçek browser bulunmayabilir -> `--no-browser` ve kontrollü açıcı fixture; gerçek masaüstü deneyimi ayrı belirtilir.
- Linux'tan Windows build'i çalışma zamanı davranışını kanıtlamaz -> fake-env testleri ve cross-build ayrı kanıtlanır; yerel Windows smoke yapılmadıysa belirtilir.
- İki SQL kopyası zamanla ayrışabilir -> build/test doğrulamasında içerik eşitliğini kontrol et.

## Migration Plan

Mevcut kullanıcı verisini koru; yeni profilde ilk migration'ı uygula, mevcut profilde version kaydını kullan. Foundation değişiklikleri mevcut metadata'yı resetlemez. Başarısız startup açık hata ile durur; recovery için bozuk DB'yi yerinde korur. Installer/release rollout bu change'in kapsamı değildir.
