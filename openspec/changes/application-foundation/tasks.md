# Tasks: application-foundation

Mevcut iskelet korunarak tamamlanır. İşaretler dosya varlığından veya eski tamamlandı iddiasından değil, belirtilen doğrulama kanıtından türetilir. Go 1.26.10 ile güncel `CGO_ENABLED=0 go test -v ./...`, `go vet ./...`, Linux/Windows cross-build ve TypeScript/Vite production build başarılıdır. Repository görünmeyen ve yalnızca loopback ağı bulunan bwrap ortamında binary/index/asset/health, boş/dolu 17321, browser fixture, SIGINT/SIGTERM, yeniden açılış, DB koruması ve log hatası smoke geçmiştir. Kanıt `.git/openspec-orchestration/foundation-implementation.md` ve eşlik eden loglardadır; temiz candidate checkout doğrulamasını orkestratör tamamlayacaktır. Açık maddeler yeniden yazım talebi değil, doğrulama veya eksik tamamlama işidir. GitHub Actions/release kapsamı change 12'ye bırakılmıştır.

## 1. Repo iskeleti

- [x] 1.1 Mevcut `cmd/engelsiz/main.go`, `internal/{app,api,config,database,web}`, `frontend/`, `migrations/` temelini koru; temiz checkout kaynak listesi ve executable build ile entrypoint'in teslim edildiğini doğrula.
- [x] 1.2 `go.mod` minimum Go 1.26.0 ve cgo'suz SQLite bağımlılığını, React/TS/Vite/Tailwind package girdilerini doğrula; dosya incelemesi ve Go 1.26.10 baseline testleri mevcut bağımlılıkları doğruluyor.
- [x] 1.3 `.gitignore` kurallarını kaynak entrypoint ve embed `.gitkeep` dosyasını dışlamayacak şekilde düzelt; `git check-ignore --no-index` ile kaynakların dahil, üretilen binary/asset/DB dosyalarının hariç olduğunu doğrula.

## 2. Config ve dizinler

- [x] 2.1 Windows app-data ve Linux XDG çözümlemesini design'daki köklere göre doğrula/tamamla; fake-env testleriyle standart fallback'leri, mutlak kökleri ve home hatasında executable/current directory'ye yazılmadığını kontrol et.
- [x] 2.2 Startup'ta config/log/download/content/runtime/cache dizinlerini oluştur; geçici boş profilde dizin varlığı ve uygun kökler testini çalıştır.

## 3. Database ve migration

- [x] 3.1 SQLite WAL ve busy timeout ayarlarını doğrula; açılan bağlantıda PRAGMA değerleri ve cgo kapalı build/test sonucuyla tamamlandığını kanıtla.
- [x] 3.2 Versioned transaction'lı migration runner'ı doğrula; fresh/idempotent ve başarısız migration testleriyle kısmi uygulama kalmadığını kontrol et, root ve embedded SQL kopyalarının eşitliğini doğrula.
- [x] 3.3 `settings` ve `providers` şemasını koru; şema testi ve bozuk DB testinde dosya içeriğinin aynen korunduğunu, startup hatasının kaydedildiğini doğrula.

## 4. HTTP server ve lifecycle

- [x] 4.1 Yalnızca `127.0.0.1` dinle; tercih edilen boş/dolu port testleriyle `17321` ve fallback davranışını, gerçek adres logunu doğrula; test atlandıysa completion sayma.
- [x] 4.2 `GET /api/health` yanıtını `200 {"status":"ok"}` olarak koru; handler ve aynı-origin binary smoke ile JSON'un frontend tarafından gölgelenmediğini doğrula.
- [x] 4.3 Dizin/log -> DB/migration -> listen -> self-health -> browser sırasını doğrula; açıcı fixture/dublörüyle health öncesi veya başarısız startup'ta açılmadığını, başarıda gerçek URL'yi ve `--no-browser` davranışını kontrol et.
- [x] 4.4 SIGINT/SIGTERM/context iptalinde ve startup hatasında HTTP/SQLite kaynaklarını temizle ve tamamlanan OS açıcılarını asenkron reap et; browser'ı öldürme veya kapanışını bekleme; erken iptal testi ve process smoke ile portun serbest kaldığını, yeniden açılışın başarılı olduğunu doğrula.
- [x] 4.5 Temel dosya loglamasını tamamla; normal lifecycle/DB hatası loglarını ve log dosyası açılamadığında açık hata bildirimini test veya kontrollü smoke ile doğrula.

## 5. Frontend temeli ve embed

- [x] 5.1 React/TS/Vite/Tailwind tema, alias ve shadcn/ui temel tooling entegrasyonunu doğrula/tamamla; kurulum metadata'sı, tema dosyaları ve production build ile kontrol et; business sayfası ekleme.
- [x] 5.2 Yerel build scriptiyle frontend çıktısını `internal/web/dist` içine kopyala ve göm; temiz checkout build'i ve kaynak dizini dışında çalışan binary'den index/gerçek asset smoke ile doğrula; placeholder/skip yeterli değildir.
- [x] 5.3 `npm run build` içindeki TypeScript kontrolü ve Vite production build'ini doğrula; orkestratör önceki başarılı build sonucunu bildirdi. CI adımı bu change'e eklenmez.

## 6. Bütünleşik doğrulama

- [x] 6.1 Frontend/embed üretiminden sonra `go test ./...`, `go vet ./...`, `gofmt -l` ve `CGO_ENABLED=0` ile Linux/Windows executable build'lerini çalıştır; komut/çıktı sonuçlarını ve varsa atlanan testleri kaydet.
- [x] 6.2 Temiz profil, gerçek index/asset/health, bağımsız/offline binary, dolu port, browser fixture, SIGINT/SIGTERM ve yeniden açılış smoke sonuçlarını kaydet; Windows'ta çalıştırılmayan kontrolleri açıkça belirt.
- [x] 6.3 Foundation kapsamını kaynak incelemesiyle kontrol et; `internal/` yalnızca app/api/config/database/web içeriyor ve Kiwix/ZIM/download/search/update business kodu veya provider/plugin sistemi bulunmuyor.

Orkestratör temiz aday doğrulaması: kaynak ağaç `02a8cece8afb15e0be75bc3db7f0ddf738585051` geçici index ile dışa aktarıldı; kaynak entrypoint ve `.gitkeep` dahil. Temiz dizinde `npm ci`, production/embed build, CGO kapalı Go test/vet ve Linux/Windows amd64 build başarılı; Linux testlerinde skip yok, npm audit sıfır açık. Adaydan üretilen Linux binary ayrıca kaynak dizini bulunmayan ve yalnızca loopback ağı olan sandbox içinde index/asset/health, port fallback, browser fixture, signal/restart, veri koruma ve log hatası smoke testlerinden geçti. Windows executable üzerinde çalışma testi yapılmadı.
