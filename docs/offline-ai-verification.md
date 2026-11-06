# Yerel AI doğrulama matrisi

Bu dosya test planıdır. Bir senaryo ancak komut çıktısı veya gözlenen çalışma sonucu kayıtlıysa doğrulanmış sayılır.

| İddia | Kanıt | Sınır |
|---|---|---|
| AI isteğe bağlı | Modelsiz başlangıç/API testi, ağ çağrısı ve inference süreci olmaması | Gerçek paketli uygulama ayrıca kontrol edilir |
| Güvenilir kurulum | Sabit URL/boyut/SHA-256, bozuk ve yarım dosya fixture'ları, iptal/yeniden deneme/kaldırma | Küçük fixture gerçek çok GB aktarım değildir |
| CPU ve süreç yaşam döngüsü | Yerel sahte server ile argüman/auth/stream/iptal/tek oturum/kapanış testleri | Gerçek llama.cpp ve GGUF testi yerine geçmez |
| Offline RAG | Yalnızca kurulu içerik, bounded arama/okuma, gerçek kaynak URL'leri ve yetersiz sonuç testleri | Gerçek ZIM ile Türkçe arama ayrıca ölçülür |
| Arayüz | TypeScript/Vite, tarayıcıda modelsiz durum, fixture streaming ve iptal | Model kalitesini ölçmez |
| Mevcut modüller | Go genel testleri, ilgili race testleri, frontend build | Haritaların tüm donanım kombinasyonlarını kapsamaz |
| Windows/Linux | Native Linux testleri ve Windows cross-build; varsa ayrı Windows runtime testi | Cross-build Windows üzerinde çalıştırma değildir |
| Gizlilik | Loopback/auth/origin testleri, inference için dış ağ kullanmayan kod yolu | Model/kaynak ilk indirmesi internet gerektirir |

## Gerçek donanım deneyi

Kullanıcının açık onayıyla model ve runtime indirildikten sonra her profil için şunları kaydedin:

1. İşletim sistemi, CPU, toplam RAM, GPU ve llama.cpp sürümü.
2. GGUF boyutu/SHA-256, context sınırı, CPU/GPU çalışma parametreleri.
3. Model yükleme süresi, ilk token süresi, token/s ve tepe RSS.
4. İndirilen Türkçe Wikipedia üzerinde aynı soru, kullanılan kaynaklar ve internet kapalı çalışma.
5. Yanıtı durdurma ve uygulamayı kapatma sonrasında kalan süreç/RAM kontrolü.

4 GB fiziksel RAM veya sınırlandırılmış ortam testi yapılmadıysa Lite için 4 GB uyumluluğu doğrulandı denmez. GPU fallback gerçek GPU hata senaryosu olmadan donanım üzerinde doğrulanmış sayılmaz.

## Gözlenen kanıtlar (6 Kasım 2026)

- Model ve motor boyutları/hash'leri, sabit Hugging Face revision metadata'sı ve GitHub release digest'leriyle ana oturumda bağımsız doğrulandı. Gerçek GGUF ağırlıkları veya llama.cpp arşivleri indirilmedi.
- Frontend'in 19 asistan testi geçti: Unicode/SSE parça sınırları, CRLF, `done` olmadan kesilme, iptal, eski isteğin yeni sonucu ezmemesi, kaynak güvenliği, HTML kaçışları ve açık kurulum niyeti.
- Chromium'da yerel fetch fixture'ıyla Türkçe delta, kaynak bağlantısı ve **Durdur** sonrası abort/eksik yanıt gözlendi. Bu bir model kalite veya gerçek Kiwix testi değildir. 390×844 mobil görünümde yatay taşma yok; düğmeler ekran sınırları içinde.
- Windows amd64 engine test binary'si Wine 11.21 üzerinde çalıştırıldı: 14 test geçti. Gerçek alt süreç başlatma/stream/iptal/kapanış, auth reddi ve fixture GPU fallback senaryoları kapsandı. Wine sonucu gerçek Windows veya gerçek GPU kabulü değildir.
- Windows amd64 model yöneticisi test binary'si aynı Wine ortamında çalıştırıldı: 20 test geçti. İç içe arşiv yolları, EXE/DLL fixture kurulumu, bozuk hash, kaldırma/yeniden kurma ve kapanışta inference bekleme kapsandı. İlk denemede Unix dosya adları kullanan dört test düzeltildi; Windows'ta `.exe` beklentisi kaldırılmadı.
- Genel frontend testi: `node --test frontend/tests/*.test.mjs`, **58/58 geçti**. `bash scripts/build-frontend.sh` TypeScript/Vite derlemesini ve yerel production embed kopyasını tamamladı; asistan ayrı lazy chunk olarak yüklendi.
- Paketleme/güvenlik Python testleri: `python -m unittest discover -s scripts/tests -v`, **55 geçti, 3 atlandı**. Atlanan üç test mevcut Argos gerçek çeviri motoru/modellerini gerektirir; bu ortamda kurulu değildir. Bunlar AI asistanı testi değildir.
- Genel backend: `go test ./...`, **18 pakette 567 test geçti**. CI kapsamındaki race komutu (app/packages/download/runtime/translation/education/api/assistant), **10 pakette 390 test geçti**. `go vet ./...` başarılı. Linux ve Windows amd64 uygulamaları `CGO_ENABLED=0` ile derlendi.
- Uygulama attribution envanteri gerçek Go 1.26.0 binary'siyle yenilendi ve `collect-app-notices.py --check` geçti. Sistem `go` launcher'ı tek başına `GOTOOLCHAIN=local` altında 1.25.4 seçtiğinden komutta `--go "$(go env GOROOT)/bin/go"` kullanıldı; güvenlik kontrolü gevşetilmedi.
- Gerçek Linux binary'si, boş ve yalıtılmış XDG dizinleriyle modelsiz açıldı: `/`, `/api/health` ve `/api/assistant` 200; üç model `missing`, motor `missing`, `busy:false`, `/api/downloads` boş. Yabancı Origin 403 ile reddedildi. Embedded arayüzde **Asistan** ekranı açıldı; bu bir mock API testi değildir.
- Aynı modelsiz boşta süreç örneğinde `/proc` RSS **16.780 KiB (yaklaşık 16,4 MiB)**, 8 thread, alt süreç listesi boştu. Tek anlık ölçümdür; tepe bellek veya inference RAM ölçümü değildir.
- Aynı smoke uygulaması `SIGINT` ile kapatıldı; `shutdown complete` ve exit 0 gözlendi. Süreç yokluğu ve yerel portun kapandığı doğrulandı. Geçici Vite fixture sunucusu da kapatıldı.

## Review-fix sonucu

Tur sayısı: **1**. Son review: **temiz**. OpenSpec: **yok** (çalışma diff'inde aktif change yoktu).

Reviewer sonucu: “Yüksek güvenli bug/güvenlik/regression bulgusu bulunamadı.” Temiz sonuç nedeniyle debug döngüsü başlatılmadı. Öncesindeki artifact doğrulamalarında yanlış revision aktarımı, tek etkin modelin kaldırılamaması, nested runtime yolu, kapanışta inference bekleme, public health endpoint'ine dayanan kimlik varsayımı, React render sırasında stream iptali, encoded href separator, ilgisiz HTML ve context bütçesi sorunları testlerle düzeltildi.

Test ortamı Linux amd64, Go 1.26.0, Node 26.12.1, yaklaşık 15,3 GiB toplam RAM'dir. Bu değerler test makinesini tanımlar; modelin ölçülmüş RAM gereksinimi değildir. Gerçek model tepe RSS, token/s ve ilk token süresi ölçülmemiştir.

## Gerçek motor kurulumu sonrası düzeltme

İlk teslimden sonra kullanıcı motoru indirdi. Gerçek Linux b12174 arşivinin 23.047.184 bayt ve katalogla aynı SHA-256 olduğu doğrulandı. Kurulum hatasının nedeni bozuk indirme değil, arşivde hedefinden önce sıralanan zincir sembolik bağlantılardı. Bağlantılar artık önce güvenli biçimde oluşturuluyor, ardından bütün zincirler staging sınırı içinde çözümlenerek doğrulanıyor; hash veya path güvenliği gevşetilmedi.

`TestChainedSymlinkForwardOrder` düzeltmeden önce başarısız, sonra başarılı oldu. Asistanın race testleri ve `go test ./internal/assistant ./internal/api ./internal/app` geçti. Kullanıcının mevcut dosyası yeniden indirilmeden kuruldu: canlı API `runtime.status=ready`, `available=true`, `error=null` döndü. Gerçek kurulu `llama-server --version` exit 0 ile `0.9.0-dev (build 12174, commit 18ab62a8e)` bildirdi. Bunlar gerçek Linux motor kurulum/çalıştırma kanıtıdır; henüz gerçek model yanıt kalitesi veya inference performansı ölçümü değildir.
