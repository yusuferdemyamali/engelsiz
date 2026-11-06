# Offline Eğitim Merkezi

Bu modül, ana spesifikasyondaki ilk v0.1 kapsamını PDF ve doğrulanmış tek dosyalı HTML5 eğitim içerikleriyle genişletir. Mevcut Wikipedia, harita ve çeviri modüllerinin yerini almaz. Kolibri çalıştırıcısı veya ikinci bir ZIM okuyucusu eklemez.

## Mimari ve veri sınırları

- Katalog uygulamayla gelen yerel `internal/education/catalog.json` manifestidir. Listeleme veya kurulu içeriği açma sırasında uzak katalog sorgusu yapılmaz.
- İndirme, mevcut `internal/download` yöneticisini kullanır. Duraklatma, devam, iptal ve yeniden deneme mevcut indirme API'sinden yapılır. Tarayıcı sunucuya indirme URL'si veya yerel dosya yolu vermez.
- PDF ve HTML dosyaları mevcut veri kökünün `content/education` alanında tutulur. Başarılı transfer tek başına kurulum değildir: boyut, SHA-256 ve dosya türü doğrulaması gerekir.
- Eğitim ZIM'leri mevcut `internal/packages` kurulumunu ve tek Kiwix sürecini kullanır. `content_type=education` ve kaynağa özgü `edu-` varyantları Wikipedia'nın etkin paket seçimini değiştirmez.
- Eğitim içeriği silme işlemi yalnızca ilgili kaydın sahip olduğu dosyalara uygulanır. Wikipedia ve harita dosyaları bu işlemin hedefi değildir.

## Lisans politikası

Ücretsiz erişim, yeniden dağıtım izni değildir. Her indirilebilir kaydın özgün kaynağı, kullanım koşulları, atfı, sürümü, kesin boyutu ve SHA-256 değeri bulunmalıdır. Eğitim materyallerinin kendileri kurulum paketine gömülmez; kullanıcı isterse doğrulanmış kaynaktan indirir. Engelsiz'in MIT lisansı bu materyallerin lisansını değiştirmez.

İzin, doğrudan indirme veya çevrimdışı uygunluğu doğrulanamayan kaynaklar yalnızca harici başvuru olarak gösterilir. Bu bağlantıların internet gerektirdiği arayüzde belirtilir. Güvenlik açısından kritik materyallerde kaynak tarihi ve profesyonel eğitimin yerini tutmadığı açıkça gösterilir; uygulama kendi tıbbi talimatlarını üretmez.

## Görüntüleme ve güvenlik

PDF, uygulama içindeki tarayıcı PDF görüntüleyicisinde açılır; tarayıcının PDF desteği gereklidir. ZIM, mevcut `/content/` Kiwix proxy'sinden açılır. HTML5 yalnızca incelenmiş, tek dosyalı içerik için desteklenir; genel ZIP veya uzaktaki web sitesini çevrimdışı kopyalama desteği yoktur.

Yerel PDF/HTML yolu `/education/content/{id}` biçimindedir; istemci dosya yolu seçemez. HTML iframe'i `allow-scripts` ile yalıtılır, `allow-same-origin` verilmez. Sunucu CSP'si uzak bağlantıları engeller. Dosya erişimi kökle sınırlandırılır; yanlış tür, eksik dosya ve bütünlüğü bozulmuş kurulum kullanılabilir olarak sunulmaz.

PhET'in incelenen dosyaları dinamik JavaScript üretimi gerektirir: `script-src 'unsafe-inline'` tek başına `EvalError` ile başlatmayı durdurur. Yalnızca yalıtılmış eğitim HTML yanıtında `unsafe-eval` gerekir; uygulamanın kendi sayfasına uygulanmaz. `connect-src 'none'`, uzak script/görsel yasağı ve opak sandbox origin'i korunur. Bu istisna genel veya kullanıcıdan alınan rastgele HTML çalıştırma izni değildir.

İndirme isteğinde `Accept: */*` ve `Accept-Encoding: identity` gönderilir. PhET'in CDN'i `Accept` bulunmadığında HTML'e ek bir izleme script'i yerleştirebildiği için bu başlık önemlidir. Eklenmiş sürüm, boyut/SHA-256 kontrolünden geçmez; kontroller gevşetilmez ve dosya uygulama tarafından değiştirilmez. Mevcut transfer yöneticisinin Range/If-Range ve boyut kontrolleri korunur.

## Değişen dosyalar

| Alan | Dosyalar |
|---|---|
| Frontend ve navigasyon | `frontend/src/education.tsx`, `frontend/src/App.tsx`, `frontend/tests/education.test.mjs` |
| Yerel katalog, kurulum ve doğrulama | `internal/education/catalog.go`, `catalog.json`, `catalog_test.go`, `service.go`, `service_test.go`, `receipt.go` |
| API ve uygulama bağlantısı | `internal/api/education.go`, `education_test.go`, `internal/app/app.go` |
| ZIM silme ve transfer | `internal/packages/education.go`, `education_test.go`; `internal/download/transfer.go`, `manager_test.go` |
| Mevcut SQLite genişletmesi | `internal/database/migrations/007_education.sql`, `008_education_tombstones.sql`, bunların kök `migrations/` kopyaları; `internal/database/db_test.go` |
| Belgeleme ve envanter | Bu belge, `docs/main-spec.md`, `third_party/application-notices/inventory.json` |

İkinci migration, silinmiş bir kaynağın eski tamamlanmış indirme kaydından tekrar görünmesini önleyen transfer mezar taşlarını tutar. Yeni veritabanı veya ayrı servis yoktur. Attribution envanterindeki değişiklik yalnızca yeni `internal/education` paketinin Linux/Windows paket listelerine eklenmesidir; bağımlılık/lisans metinleri değişmemiştir.

## Doğrulanmış başlangıç kaynakları

2026-11-06 tarihinde özgün dosyalar HTTP üzerinden alınmış; aşağıdaki kesin boyut ve SHA-256 değerleri yerel dosyalar üzerinde ayrıca doğrulanmıştır. Tam URL/hash/atıf bilgileri manifestte tutulur. Değişen bir uzak dosya aynı kaydın yerine sessizce kabul edilmez; katalog pini yeniden incelenmelidir.

| İçerik | Dil / biçim | Boyut | Lisans / sürüm |
|---|---|---:|---|
| WHO: Water, sanitation and hygiene in health care facilities | İngilizce PDF | 4.518.452 B | CC BY-NC-SA 3.0 IGO; 2019 |
| WHO: Guidelines for drinking-water quality | İngilizce PDF | 5.321.705 B | CC BY-NC-SA 3.0 IGO; 4. baskı + iki ek, 2022 |
| WHO: Kendi Kendine Yardım ve Fazlası (KY+) | Türkçe PDF | 5.262.381 B | CC BY-NC-SA 3.0 IGO; Türkçe 2023-01-24, saha sürümü 2021 |
| Türkçe Vikikitap | Türkçe ZIM | 26.888.953 B | CC BY-SA 4.0 / kimi metinler GFDL; medya özel lisansları saklı; 2026-09 |
| OpenStax: Anatomy & Physiology 1e | İngilizce PDF | 135.405.076 B | Bu özgün PDF'de CC BY 4.0; ©2017, asıl baskı 2013 |
| PhET: Enerji Çeşitleri ve Dönüşümleri | Türkçe HTML5 | 3.524.853 B | CC BY-NC 4.0; 1.4.26 |
| PhET: Molekül Oluştur | Türkçe HTML5 | 8.042.999 B | CC BY-NC 4.0; 1.0.27 |
| PhET: Devre Kurulum Aracı: DC | Türkçe HTML5 | 3.509.674 B | CC BY-NC 4.0; 1.5.4 |

Önemli ayrımlar:

1. WHO KY+ bir **grup temelli stres yönetimi/kolaylaştırıcı kursudur**; ilk yardım, bireysel tedavi veya psikolojik ilk yardım kitabı olarak sunulmaz. PDF'nin telif sayfası, 2023 Türkçe sürümünü ve ticari olmayan/aynı lisans koşullarını belirtir. [Doğru yayın kaydı](https://iris.who.int/handle/10665/365640).
2. WHO ve PhET, yalnızca lisanslarına uygun **ticari olmayan eğitim kullanımına** sunulur. Ticari kullanım için hak sahibinden ayrıca izin gerekir. Uygulamanın MIT lisansı bu koşulu kaldırmaz. Gömülü lisanslar değiştirilmez; PhET atfı kullanım noktasında da gösterilmelidir.
3. WHO su/hijyen belgeleri tesis planlaması ve teknik su güvenliği kaynaklarıdır; kısa evde hayatta kalma kılavuzu gibi sunulmaz. [2019 WASH](https://iris.who.int/handle/10665/311618) ve [2022 içme suyu](https://iris.who.int/handle/10665/352532) PDF'lerinin telif sayfalarında ticari olmayan kopyalama/yeniden dağıtım açıkça izinlidir. Özgün PDF'ler değiştirilmez.
4. OpenStax lisansı **indirilen birinci baskının telif sayfasından** doğrulanmıştır; güncel veya farklı baskılar için aynı lisans varsayılmaz. Özgün PDF'nin sayfa atıfları korunur; logolar/kapaklar uygulama markası olarak ayrıca kullanılmaz.
5. PhET dosyalarında isteğe bağlı Google Tag Manager, telemetri ve güncelleme denetimi referansları vardır. Bunlar CSP ile engellenir; simülasyonların asıl kodu/görselleri yereldir. Uzak HTML sayfası veya `/latest` değil, tam sürüm URL'si/hash'i kullanılır. [PhET koşulları](https://phet.colorado.edu/en/licensing).

## Lisans veya teknik engelle indirilmeyen kaynaklar

AFAD/Hazırım, Kızılay Akademi, Sağlık Bakanlığı ve MEB/OGM için tüm materyalleri kapsayan yeniden dağıtım izni varsayılmamıştır. AFAD, Sağlık Bakanlığı ve MEB katalogda açıkça yalnızca internet gerektiren kaynak başvurularıdır. Kızılay'ın oturum gerektiren kursları paketlenmez. Bu nedenle Türkçe deprem/ilk 72 saat/temel yaşam desteği/kanama-yanık-kırık gibi içeriklerin **çevrimdışı kapsamı henüz tamamlanmış değildir**. Açık izin ve güncel belge doğrulaması gerekir.

iFixit'in ticari olmayan/aynı lisans koşulları bulunmasına rağmen incelenen PDF adresleri bağımsız doğrulanmış belge vermedi; yalnızca başvuru olarak listelenir. Appropedia'nın açık içerik lisansı, tüm siteyi tek dosyada çevrimdışı sunmayı otomatik olarak sağlamaz; mevcut kataloğa incelenmemiş bir kopya eklenmez.

CDC'nin iki küçük su güvenliği PDF'si indirilerek hash'leri doğrulanmış olsa da [CDC kullanım koşulları](https://www.cdc.gov/other/agencymaterials.html), içeriğin özgün sitede yeni pencerede açılmasını ve başka site içinde çerçevelenmemesini belirtir. Yerel kopyaya açık bir istisna bulunmadığından **uygulama içi indirme kapatılmış**, yalnızca harici başvuru bırakılmıştır. Kamu malı olması bu görüntüleme koşulunu yok saymak için kullanılmaz. WHO 2013 evsel arıtma katılımcı el kitabı ise “All rights reserved” nedeniyle dahil edilmemiştir.

## Kolibri uygulanabilirlik kararı

[Kolibri](https://learningequality.org/kolibri/) çevrimdışı sınıf/LAN sunucusudur; sıradan bir PDF, ZIM veya tek HTML dosyası değildir. [Kurulum](https://kolibri.readthedocs.io/en/latest/install/ubuntu-debian.html) ve [kanal yönetimi](https://kolibri.readthedocs.io/en/latest/manage/resources.html) ayrı servis, yaşam döngüsü, depolama ve paketleme gerektirir. Engelsiz'e ek servis zorlanmamıştır.

[Kolibri Studio kamu API'sinde](https://studio.learningequality.org/api/public/v1/channels) Türkçe kanallar görülmüştür: Khan Academy (Türkçe), `59b3f5364f60569886baa64f6ae147d1` (26.651 kaynak; yaklaşık 36,6 GB; kanal v1, 2025-12-26) ve Sikana (Türkçe), `cfa63fd45abf5b7390b1a41f3b4971bb`. Kanal metadata'sı tek başına içerik lisansı kanıtı değildir. İçerik bazlı hak incelemesi ve ayrı runtime/paketleme tasarımı yapılmadan indirilebilir veya Engelsiz'de çalışır olarak işaretlenmez.

## Doğrulama durumu

**2026-11-06: uygulanabilir modül tamamlandı ve gerçek ağsız senaryo doğrulandı.** Bu sonuç aşağıda belirtilen içerik/hukuk/platform sınırlarını kaldırmaz.

### Otomatik kontroller

| Kontrol | Sonuç |
|---|---|
| `node --test frontend/tests/*.test.mjs` | 39 başarılı; 15'i eğitim arayüzü/eylemleri/erişilebilir durumları/atıf/stale-response testleri |
| `bash scripts/build-frontend.sh` | TypeScript kontrolü, Vite üretim derlemesi ve Go embed kopyası başarılı |
| `go test ./...` | 15 pakette 486 test başarılı |
| `go vet ./...` | Başarılı |
| `go test -race ./internal/app ./internal/packages ./internal/download ./internal/runtime/kiwix ./internal/translation ./internal/education ./internal/api` | 7 pakette 324 test başarılı |
| `python -m unittest discover -s scripts/tests -v` | 55 test: 52 başarılı, 3 çeviri runtime testi ortamda Argos bulunmadığından atlandı |
| `python scripts/collect-app-notices.py --check --go "$(go env GOROOT)/bin/go"` | Go 1.26.0 çalıştırıcısıyla başarılı; bağımlılık/lisans metinleri değişmedi |
| `GOOS=windows GOARCH=amd64 go build ./cmd/engelsiz` | Windows x64 çapraz derlemesi başarılı; Windows üzerinde çalıştırma/kurulum testi değildir |
| `git diff --check`, değişen Go dosyalarında `gofmt -l` | Temiz |

Projede ayrı bir frontend lint script'i yoktur; olmayan bir lint komutu çalıştırılmış gibi raporlanmaz. Mevcut harita üretim parçasının 500 kB uyarısı sürer; yeni bir bağımlılık eklenmemiştir. Testlerde bozuk/kesik dosya, yanlış tür, sahte receipt, symlink, yol geçişi, yanlış origin/host, yabancı Wikipedia paket ID'si, ZIM kurulum hatası, silme kesintisi, yeniden indirme ve DB hata durumları kapsanır. İlk yarış testinde görülen iki zamanlama hatası düzeltildikten sonra yukarıdaki son sonuçlar alınmıştır.

### Gerçek içerikle online → offline kanıtı

Ortam: Linux `7.3.0-2-cachyos-bore`, Chromium `156.0.8078.79`, Go `1.26.0`; gerçek Kiwix `3.8.2` (libkiwix 14.2.0 / libzim 9.5.0). Test edilen Go uygulama binary'sinin SHA-256 değeri `69beb15ba8df60661f6be1e2c424a29ba645b245cf4fa97da095114494fadb81`.

1. Kullanıcının normal veri dizinine dokunmadan ayrı XDG test dizininde **üç WHO PDF'si, Türkçe Vikikitap ZIM'i ve üç PhET HTML dosyası**, uygulamanın gerçek API'sinden indirildi/kuruldu. İlerleme durumları, Vikikitap için duraklat/devam (`200/200`), dosya boyutları ve SHA-256 değerleri doğrulandı. Kiwix gerçek arşivi etkinleştirdi. 14 katalog kaydı ve 8 indirilebilir kaynak listelendi.
2. Uygulama tamamen durduruldu. Aynı veri dizini, `unshare --user --map-root-user --net` içinde yalnızca loopback açıkken kullanıldı. Yönlendirme tablosu boştu; hem dış IP'ye TCP hem dış HTTPS erişimi gerçekten başarısızdı. **Uygulama ve Chromium bu ağsız ortamda yeni süreçler olarak başlatıldı.**
3. Kayıtlar korundu; PDF uygulama iframe'i içindeki gerçek Chromium PDF eklentisinde açıldı. Türkçe Vikikitap ana sayfası ve `Çağdaş_Yunanca` makalesi mevcut `/content/` proxy'siyle okundu. Üç PhET'in gerçek `phet.joist.sim` nesnesi oluştu ve ekran durumu değiştirildi; yalnızca HTML dosyasının HTTP 200 dönmesi başarı sayılmadı.
4. Üç PhET için parent/iframe belge erişimi engellendi. Telemetri/uzak script/güncelleme istekleri CSP tarafından engellendi; **başarıyla tamamlanmış dış HTTP isteği yoktu**. Çevrimdışı Wikipedia katalog yenilemesi `503` verebilir; bu mevcut ayrı katalog isteğinin hatası eğitim listesi/okuyucusunu engellemedi. Konsoldaki beklenen CSP/503 mesajları sıfır hata diye gizlenmedi.
5. Önce bir PDF, ayrı devam testinde bir HTML ve ZIM kaldırıldı. İlgisiz kalan içeriklerin hash'leri değişmedi; ZIM dosyası silindi, etkin Kiwix içerik listesi boşaldı. Ağsız ortamda yeniden başlatınca üç silme de kalıcıydı. Masaüstü 1280 px ve mobil 390 px arayüzlerinde yatay taşma yoktu; mevcut tema, dört kategori, dil/indirme durumları ve okuyucu atıfları kontrol edildi.

Oturumun ham JSON/ekran kanıtları `/tmp/opencode/education-acceptance/` altında `report-online.json`, `report-offline.json`, `report-offline-removal.json` ve PNG dosyaları olarak üretildi; bunlar üçüncü taraf içeriklerle birlikte uygulama paketine eklenmez. Kaynak araştırması sırasında elde edilen PDF/HTML/ZIM dosyaları da repository'ye konulmadı.

### Açık sınırlar ve eksikler

- Türkçe resmî afet/ilk yardım kapsamı tam değildir: deprem, ilk 72 saat, afet çantası, kanama/yanık/kırık ve temel yaşam desteği için güncel ve açık izinli dosyalar hâlâ gereklidir. Başvuru bağlantıları çevrimdışı içerik gibi gösterilmez. WHO KY+ ve su rehberleri bu eksiklerin yerine geçirilmez.
- OpenStax'ın 135 MB dosyası kaynağından ayrıca indirilip hash/lisansı doğrulanmıştır; bu oturumdaki yedi dosyalık uygulama içi uçtan uca ve ağsız kabul döngüsüne dahil edilmemiştir. Diğer PDF'lerle aynı doğrulanmış kurulum/okuyucu yolunu kullanır.
- WHO ve PhET için ticari olmayan kullanım sınırı geçerlidir; ticari kullanım izni alınmış değildir. Katalog sabittir, otomatik tıbbi güncellik garantisi veya sessiz kaynak güncellemesi yoktur.
- Kolibri çalıştırılmaz; ayrı servis/paketleme ve kanal içeriği lisans denetimi gerekir. Türkçe kanalların bulunması entegrasyonun tamamlandığı anlamına gelmez.
- Windows temiz makine/yerel tarayıcı testi ve Chromium dışındaki PDF okuyucuları bu oturumda doğrulanmadı. Mevcut Wikipedia/harita testleri başarılıdır; büyük Wikipedia indirmesi ve 81 il haritasının yeniden indirilmesi yapılmadı. Önceden bekleyen release kabul maddeleri otomatik kapatılmadı.

Kullanıcı tekrarı: uygulamayı açın → **Eğitim** → izinli bir PDF/ZIM/HTML indirin ve açın → uygulamayı kapatıp interneti kesin → yeniden açıp aynı içeriği açın. Yerel Kiwix çalıştırıcısı geliştirme ortamında `--kiwix-binary /tam/yol/kiwix-serve` ile sağlanır; dağıtım paketlerinin mevcut runtime kuralları değişmemiştir.
