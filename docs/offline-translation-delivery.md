# Çevrimdışı Türkçe ↔ İngilizce çeviri — teknik teslim

Tarih: **2026-11-04**. **Linux'ta gerçek paket ve gerçek offline kullanım doğrulandı. Windows native çalıştırma/installer kabulü doğrulanmadı.** Bu rapor bir yayımlanmış release veya bütün platformlar için eksiksiz kabul beyanı değildir.

## 1. Git ve değişen dosyalar

- Branch: `feat/offline-tr-en-translation`.
- Uygulama/test/paketleme commit'i: **`8e52271106dd6c2f5709df1f6afbea2739ebb2f3`** (`feat: add offline Argos Turkish-English translation`). Bu rapor ve kullanıcı belgeleri ayrı bir dokümantasyon commit'inde tutulur; tam teslim commit çiftini `git log -2 --oneline` gösterir.
- Başlangıçtaki `main` ve alınan `origin/main`: `94200c9`. `main` üzerine merge veya push yapılmadı. Başlangıçtaki commit edilmemiş `.openchamber/` kullanıcı çalışması değiştirilmedi ve commit edilmedi.

| Alan | Önemli dosyalar / sorumluluk |
| --- | --- |
| Go servis ve motor | `internal/translation/{service,worker,catalog,pins,extract,receipt}.go`: bağımsız yönler, sabit acquisition metadata, güvenli kurulum, restart ve worker lifecycle |
| API ve kalıcılık | `internal/api/translation.go`, `internal/app/app.go`, `internal/database/migrations/006_translation.sql` ve kök migration kopyası |
| Arayüz | `frontend/src/translation.tsx`, `App.tsx`, `downloads.tsx`, `frontend/tests/translation.test.mjs` |
| Native dağıtım | `scripts/translation/`, `scripts/release.py`, `scripts/tests/test_translation_runtime.py`, `.github/workflows/release.yml`, `third_party/translation-runtime-lock.json` |
| Standartlar / kanıt | `openspec/changes/offline-tr-en-translation/`, `docs/translation-models.md`, `docs/main-spec.md`, README'ler, katkı ve release doğrulama rehberleri |

Tüm frontend çalışmaları designer tarafından **design-taste-frontend** ile yapıldı. Mevcut post-apokaliptik token/component sistemi korundu; yeni npm tasarım bağımlılığı eklenmedi. Go/frontend notice metinleri değişmedi; yeni `archive/zip` ve çeviri paketinin kaynak grafiği için attribution envanteri yenilendi.

## 2. Motor, modeller ve sözleşme

**Argos Translate 1.11.0**, **CTranslate2 4.8.5**, **SentencePiece 0.2.2**, **NumPy 2.4.6**, PyYAML 6.0.3 ve packaging 25.0 kullanılır. Doğrulanan Linux frozen motor **CPython 3.11.14 / PyInstaller 6.16.0** içerir; sistem Python'ı veya pip çağrılmaz.

| Yön | Argos paket sürümü | Dosya | Boyut |
| --- | --- | --- | ---: |
| tr → en | 1.5 | `translate-tr_en-1_5.argosmodel` | 120561223 bayt |
| en → tr | 1.5 | `translate-en_tr-1_5.argosmodel` | 124742526 bayt |

Gerçek URL'ler, SHA-256 değerleri, ZIP metadata kontrolü, upstream MIT/CC0 model beyanı ve eğitim verisi belirsizliği [model provenance belgesinde](translation-models.md) kayıtlıdır. Modeller temel pakete dahil değildir; her yön yalnızca açık kullanıcı eylemiyle mevcut **Download Manager** üzerinden indirilir. Ayrı downloader veya bulut fallback eklenmedi.

Go, uygulama yanındaki `runtime/translation/engelsiz-translate[.exe]` worker'ını doğrudan argv ile başlatır. Model kökü normal kullanıcı veri dizinindeki `content/translation` yoludur. Kalıcı JSONL stdin/stdout bağlantısı ve tembel yüklenen iki yönün cache'i kullanılır. Çeviri tek worker üzerinde seri işlenir; timeout 120 saniyedir. Hata/timeout/malformed yanıt worker'ı sonlandırıp reap eder; sonraki istek temiz başlangıç yapabilir. Kapanış çocuğu temizler.

Argos 1.11.0'ın Stanza/MiniSBD import/otomatik indirme kısıtları kaynakta açıklanan dar bir uyumluluk katmanıyla aşılır: gerçek Argos `Package`, tokenizer ve `apply_packaged_translation` akışı korunur, yalnızca offline cümle bölücü kullanılır. Torch, Stanza, spaCy ve MiniSBD pakete alınmaz. Python socket ve Argos indirme yardımcıları devre dışıdır; bu, taşınabilir bir kernel sandbox iddiası değildir. Gerçek ağ izolasyonu ayrıca test edildi.

API:

```text
GET  /api/translation
POST /api/translation/models/{argos-tr-en|argos-en-tr}/download
POST /api/translation/models/{id}/install       {"download_id":"..."}
POST /api/translation/translate                {"source_language":"tr","target_language":"en","text":"Merhaba"}
                                               → {"translated_text":"Hello"}
```

Girdi 5.000 Unicode karakter ve ayrı JSON byte limitiyle sınırlıdır. Desteklenmeyen çift, boş/whitespace girdi ve aşırı boyut reddedilir. Host/Origin ve JSON kontrolleri yeni endpoint'i yabancı web sitesi/DNS rebinding üzerinden tetiklemeye karşı sınırlar. Kaynak metinler loga, veritabanına veya geçmişe yazılmaz. Kurulumda sabit boyut/SHA-256, ZIP confinement, staging/atomic publish ve per-file hash receipt kullanılır; restart kurulu dosyaları doğrular. Bir yönün hatası diğerini devre dışı bırakmaz.

## 3. Windows/Linux paketleme

**Gerçek oluşturulan Linux aday paketi:**

```text
dist/translation-verification/release-final/engelsiz-linux-x64.tar.gz
446062673 bayt (yaklaşık 425,4 MiB)
SHA-256: be4761ab9ee0c8eb47c7f4545c7f9d5daa283d8cc1b8e3ed140538b301717f5f
```

Bu yerel aday, mevcut source-built Kiwix compliance girdileriyle `scripts/release.py` üzerinden oluşturuldu; Kiwix/PMTiles kaynak ve notice kapıları atlanmadı. Model dosyalarının arşivde bulunmadığı doğrulandı. Runtime 206 dosya / 359182591 bayttır; manifest kendisi dışında 205 dosyanın boyut ve SHA-256 envanterini taşır. Worker SHA-256:

`37098f82c01b57411ec93760f107a51805d0c0b6aa860b793ccc74214c152b86`

Paketleme, prerelease Python'ı, eksik/değişmiş/ek runtime dosyalarını, beklenmeyen native bağımlılıkları ve eksik notice girdilerini reddeder. CPython LICENSE, wheel lisansları, PyInstaller bootloader metni ve native kütüphane atıfları runtime'ın `THIRD-PARTY-NOTICES` ağacındadır. Native paket sürümleri build sırasında envantere alınır; her sistem kütüphanesinin kaynak sürümü ayrıca lock-pinned değildir.

Linux Go/Kiwix/PMTiles static-ELF kontrollerini korur. Argos/Python worker için ayrı dynamic-ELF ve transitif `DT_NEEDED` kontrolü vardır. **Bu adayın baseline'ı glibc 2.35 / Ubuntu 22.04**'tür; daha eski glibc veya musl/Alpine için uyumluluk doğrulanmadı. Mevcut Ubuntu 24.04 CI native build'i manifestte daha yeni bir baseline bildirebilir; lock üst sınırı 2.39'dur. Eski static-only Linux kabul iddiası çeviri motoruna genişletilmedi.

Windows native freezer/CI ve recursive `.exe/.dll/.pyd` import kontrolü eklendi; Inno'nun mevcut recursive uygulama ağacı runtime'ı kapsar. Windows build wheel hash'leri ve Windows-only build bağımlılıkları tanımlıdır. **Windows Go cross-build başarılıdır; native frozen worker, installer çalıştırma, Windows 10/11, boşluk/Unicode kullanıcı yolu kabulü gerçek Windows üzerinde çalıştırılmadı.** Bunlar tamamlandı olarak işaretlenmez.

## 4. Testler, offline kanıt ve ölçümler

| Kontrol | Sonuç |
| --- | --- |
| `go test ./...`, `go vet ./...` | Başarılı; Wikipedia/maps/download/app/backend testleri dahil |
| `go test -race ./internal/app ./internal/packages ./internal/download ./internal/runtime/kiwix ./internal/translation ./internal/api` | Başarılı |
| Frontend üretim build'i ve `node --test frontend/tests/*.test.mjs` | Başarılı; 24/24 test. 1440 px ve 390 px canlı browser kontrolleri de geçti |
| Gerçek frozen motorla `python3 -m unittest discover -s scripts/tests -v` | 38/38 başarılı; gerçek iki modelle çeviri ve paketleme drift/native import testleri dahil, skip yok |
| Attribution, native smoke, release build, Windows Go cross-build | Başarılı; notice kontrolünde doğrudan Go 1.26.0 toolchain binary'si seçildi |

Tam testte bulunan hızlı kurulum yanıt yarışı düzeltildi; ilgili iki test 20 kez tekrarlandı. EOF kapatan eski smoke'un gizlediği persistent worker okuma kilitlenmesi düzeltildi; gerçek açık-pipe regresyon testi ve stdin açıkken her istek için yanıt bekleyen smoke eklendi. Nihai paket bu düzeltmeleri içerir.

Gerçek browser üzerinden iki model indirildi/kuruldu. İlk indirme %52,9'da duraklatıldı ve sürdürüldü. Tek yön kurulu iken çeviri çalıştı; diğer yön eksik kaldı. Yön değiştirme, çok satırlı Türkçe, temizleme, gerçek clipboard kopyala/yapıştır, 5.001 karakter sınırı, progress/install/hata durumları ve navigasyon kontrol edildi. **Nihai çıkarılmış paketle browser testi ayrıca tekrar geçti.**

### Gerçek offline / Python'sız ortam

Nihai tar.gz çıkarılıp SHA-256'sı doğrulandı. Gerçek uygulama ve frozen worker, **Ubuntu 22.04.5 LTS** container'ında `--network none --read-only` ile çalıştırıldı. Sistemde Python/pip/Go/Node bulunmadığı kontrol edildi, uygulama `PATH=/no-development-tools` ile başlatıldı. Yalnızca `lo` vardı; `1.1.1.1:443` bağlantısı **network is unreachable** döndürdü. Paket yolu `/paket/Engelsiz Çeviri`, veri yolu `/kullanıcı verisi` idi; ikisi de boşluk ve Unicode içeriyordu.

İki yön restart sonrası yeniden indirmeden algılandı; normal ve çok satırlı çeviriler, API girdi güvenlik kontrolleri ve diğer modüllerin status endpoint'leri geçti. Uygulama çıkış kodu 0; çocuk worker'ın kaldırıldığı `/proc` üzerinden doğrulandı. Bundan önce ayrı `unshare --user --map-root-user --net` testinde de gerçek uygulama çevirileri geçti. Host internetinin açık olması bu izole süreçlere dış ağ sağlamadı.

| Ölçüm | Sonuç |
| --- | ---: |
| İlk tr→en isteği (worker başlatma + import + ilk model dahil) | 309 ms |
| Sonraki tr→en örneği | 36 ms |
| Aynı worker'da en→tr ilk model yüklemesi dahil istek | 148 ms |
| Sonraki en→tr örneği | 38 ms |
| Worker RSS | Tek yön yaklaşık 186 MiB; iki yön yaklaşık 354 MiB; 8/12 thread |

Çok satırlı örnekler yaklaşık 84–86 ms sürdü. Bunlar bu makinede ölçülmüş örneklerdir; saf model yükleme süresi ayrıca ayrıştırılmadı, tüm donanımlar için benchmark/SLA değildir.

Ham yerel kanıtlar: `dist/translation-verification/evidence/` altında test logları, `runtime-manifest-summary.json`, `linux-package.json`, `final-packaged-offline.json`, `final-browser.json`. Kısa, repoda tutulan izlenebilirlik: [OpenSpec verification](../openspec/changes/offline-tr-en-translation/verification.md).

## 5. Bilinen sınırlar ve kabul durumu

1. **Windows native kabulü açık.** Cross-build ve fixture/import testleri gerçek Windows çalıştırması değildir. Temiz Windows installer/uninstall/reinstall testi yapılmadı.
2. **Linux ortam sınırı açık.** Gerçek headless Ubuntu 22.04 container/paket testi geçti; ayrı temiz masaüstü VM ve eski glibc/musl testleri yapılmadı. Önceki release kabul matrisinin PENDING satırları korunur.
3. **Model kalitesi sınırlı.** tr→en modeli `Türkçe öğreniyorum.` için yanlış `I learn English.` üretebiliyor. Bu hata saklanmadı veya bulutla düzeltilmedi; kritik metinler kontrol edilmelidir.
4. **Provenance sınırı korunur.** Upstream signed checksum yok; sabit pinler gerçek HTTPS indirmelerinden ve eşleşen mirror'dan türetildi. Model MIT/CC0 beyanı geliştirici açıklamasındadır; paket içi lisans ve eğitim verisi haklarının ayrıntıları eksiktir.
5. **Regresyon kapsamı dürüstçe sınırlıdır.** Mevcut Wikipedia/maps/download testleri, build ve navigasyon geçti. Bu görevde yeniden tam boy Wikipedia arşivi veya 81 il haritası indirilip her içerik elle dolaşılmadı; böyle bir kabul iddiası yapılmaz.

İstenen manuel metin çevirisi Linux'ta uçtan uca çalışır; modeller uygulamadan edinilir ve sonrasında gerçekten offline kullanılabilir. Hesap, history, sözlük, ses/OCR, üçüncü dil, LLM sohbeti, bulut fallback veya ilgisiz refactor eklenmedi. OpenSpec değişikliği açık doğrulanamayan platform noktaları nedeniyle arşivlenmedi.
