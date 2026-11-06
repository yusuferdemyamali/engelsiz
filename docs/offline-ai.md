# Yerel AI asistanı (1.0.0)

## Güvenlik ve ürün sözleşmesi

Engelsiz, AI kurulmadan da çalışır. Asistan ayrı ve isteğe bağlıdır; uygulamanın açılması model veya inference motoru indirmez. Kullanıcı indirme başlatırken internet gerekir. Kurulumdan sonra sorular, kaynaklar ve yanıtlar cihazdan çıkmaz. Bulut inference, araç çağırma ve bilgisayar otomasyonu yoktur.

Asistan yalnızca kurulu, desteklenen metin kaynaklarında arar. Referanslar model çıktısından değil retrieval sonuçlarından üretilir. Sonuç yoksa kaynak varmış gibi davranmaz. Kaynak metni güvenilmeyen veridir; sistem talimatı veya çalıştırılabilir komut değildir.

AI yanıtları hata içerebilir. Afet, ilk yardım ve sağlık sorularında doğrudan yerel kaynak metinlerini okuyun; AI yanıtını uzman değerlendirmesi yerine kullanmayın.

## Kurulum ve kullanım

1. Engelsiz'de **Asistan** bölümünü açın. **Motoru indir** ile küçük CPU çalışma motorunu açıkça indirin ve kurun. Bu işlem ana uygulamanın veya mevcut modüllerin çalışması için gerekli değildir.
2. Bir profil seçip **İndir** düğmesine basın. Yalnızca istediğiniz model indirilir; düşük RAM'de Lite, yeterli RAM'de Balanced başlangıç seçeneğidir. Tamamlanan dosya SHA-256 ile doğrulanır; gerekirse **Kur** ile devam edin.
3. Kurulu modeli **Etkinleştir** ile seçin. **Bilgi Paketleri** veya **Eğitim** bölümünde ilgili yerel ZIM kaynağının da kurulu olduğundan emin olun.
4. Türkçe sorunuzu yazıp **Sor** seçin. Akış sırasında **Durdur** işlemi iptal eder. Yanıtın yanındaki yerel kaynak bağlantısından asıl içeriği açın; alıntı ile üretilen yanıt farklı gösterilir.
5. Modeli değiştirmek için başka bir kurulu profili seçin. **Sil** yalnızca ilgili model kurulumunu kaldırır; Wikipedia, haritalar ve eğitim içeriklerine dokunmaz.

İndirme sırasında bağlantı gereklidir; indirme/kurulum tamamlandıktan sonra asistan için internet gerekmez. Kaynak bulunamazsa asistan yetersiz yerel bilgi olduğunu söyler. PDF'lerin OCR'ı, simülasyonların kaynak kodu veya kurulu olmayan web bağlantıları bilgi kaynağı sayılmaz.

Sohbet ekranı mesajları yalnızca bellekte tutar; bu ekrandan ayrılınca silinir. Düşük bellek tüketimi için her soru bağımsız retrieval ve inference isteğidir; önceki mesajlar yeni sorunun model bağlamına eklenmez. Önceki soruya gönderme yapmak yerine yeni soruda konuyu açıkça yazın.

## Doğrulama yaklaşımı

1. Model kurulmadan uygulama/API açılışı ve mevcut modüllerin regresyon testleri.
2. Küçük yerel HTTP fixture'larıyla model indirme, SHA-256 reddi, iptal, yeniden deneme ve kaldırma testleri. Fixture testleri gerçek model çalıştırıldığının kanıtı değildir.
3. Yerel inference protokolü, streaming, iptal, tek oturum sınırı ve süreç temizliği testleri.
4. Kurulu/kurulu olmayan içerik, sınırlı retrieval ve gerçek yerel referans testleri.
5. Frontend derlemesi, arayüz durumları ve Linux/Windows derleme kontrolleri. Gerçek Windows/GPU ve fiziksel düşük RAM testleri ayrı belirtilir.

4, 8 ve 12-16 GB RAM değerleri başlangıç hedefidir, ölçülmüş uyumluluk garantisi değildir. Gerçek model, sistem RAM'i ve inference ölçümleri yapılmadan performans iddiası verilmez.

## Model kaynakları

6 Kasım 2026 tarihinde Hugging Face dosya metadata'sından boyut, immutable revision, LFS SHA-256 ve Apache-2.0 lisans bilgisi doğrulandı. Ağırlıklar geliştirme sırasında indirilmedi.

| Profil | Model / dağıtıcı | Dosya boyutu | RAM hedefi |
|---|---|---:|---:|
| Lite | Qwen3 0.6B Q4_K_M / Unsloth topluluk quantization | 396.705.472 bayt (378 MiB) | 4 GB |
| Balanced | Qwen3 1.7B Q4_K_M / Unsloth topluluk quantization | 1.107.409.472 bayt (1.056 MiB) | 8 GB |
| Quality | Qwen3 4B Q4_K_M / Qwen resmi quantization | 2.497.280.256 bayt (2.382 MiB) | 12-16 GB |

Resmi Qwen 0.6B ve 1.7B GGUF depolarında yalnızca Q8_0 yayımlandığından düşük bellek hedefi için bu iki profilde Unsloth kullanılır. Model ailesi Qwen3'tür; topluluk quantization'ları resmi Qwen dağıtımı olarak sunulmaz.

| Profil | Sabit revision | SHA-256 |
|---|---|---|
| Lite | `50968a4468ef4233ed78cd7c3de230dd1d61a56b` | `ac2d97712095a558e31573f62f466a3f9d93990898b0ec79d7c974c1780d524a` |
| Balanced | `d7f544eead698dbd1f15126ef60b45a1e1933222` | `b139949c5bd74937ad8ed8c8cf3d9ffb1e99c866c823204dc42c0d91fa181897` |
| Quality | `bc640142c66e1fdd12af0bd68f40445458f3869b` | `7485fe6f11af29433bc51cab58009521f205840f5b4ae3a32fa7f92e8534fdf5` |

Metadata kaynakları: [Lite](https://huggingface.co/api/models/unsloth/Qwen3-0.6B-GGUF?blobs=true), [Balanced](https://huggingface.co/api/models/unsloth/Qwen3-1.7B-GGUF?blobs=true), [Quality](https://huggingface.co/api/models/Qwen/Qwen3-4B-GGUF?blobs=true). İndirme URL'leri `resolve/<revision>/<dosya>` biçiminde sabittir; `main` veya `latest` kullanılmaz.

## Inference motoru

CPU varsayılanıdır. İsteğe bağlı motor, [llama.cpp b12174](https://github.com/ggml-org/llama.cpp/releases/tag/b12174) sürümüne sabitlenir (MIT, commit `18ab62a8ec5144ae5d2f65e78374ba69652d75dd`). Bu bir geliştirme derlemesidir; resmi semver sürümleri hazır binary içermediğinden özellikle seçilmiş ve hash ile sabitlenmiştir. Kendiliğinden güncellenmez.

| Platform | Arşiv | Boyut | SHA-256 |
|---|---|---:|---|
| Linux amd64 CPU | `llama-b12174-bin-ubuntu-x64.tar.gz` | 23.047.184 bayt | `75c077cf5481264c9701851df05013c39dfe16760167eca7932af84c255e3dc5` |
| Windows amd64 CPU | `llama-b12174-bin-win-cpu-x64.zip` | 24.746.357 bayt | `b2ec458994bacf7e837dfc54e98152ea4bea97779014c858d98840d050f349d6` |

Hash kaynağı: [GitHub release API asset digest](https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/b12174). Linux hazır motoru glibc 2.35+ gerektirir; bu gereksinim ana Engelsiz uygulamasının gereksinimlerini değiştirmez. Windows'ta Visual C++ runtime gerekebilir; temiz Windows ortamında gerçek motor testi tamamlanmadı. Arşivdeki eşlik eden kütüphaneler korunmalıdır; tek başına `llama-server` dosyasını taşımak yeterli değildir.

Motor yalnızca yerel loopback adresinde, rastgele port ve istek anahtarıyla çalışır. Ortamın HTTP proxy ayarları inference isteklerinde kullanılmaz. Her yanıt bittikten veya iptal edildikten sonra süreç kapatılır; hızlı yeniden yanıt uğruna model RAM'de tutulmaz. Aynı anda ikinci yanıt üretilmez.

Lite için küçük context, daha büyük profiller için sınırlı context kullanılır; modelin on binlerce token desteklemesi bu miktarda RAM ayrılacağı anlamına gelmez. GPU'suz CPU çalışması temel yoldur; gerçek GPU/driver uyumluluğu ayrı test gerektirir.

## Geliştirici seçenekleri

Normal kullanım için ek ayar gerekmez. Kendi uyumlu llama.cpp kurulumunu kullanacak geliştiriciler `ENGELSIZ_LLAMA_BINARY` ile **mutlak** `llama-server` / `llama-server.exe` yolunu belirleyebilir. Göreli yol, PATH araması veya harici inference URL'si kabul edilmez. Özel motor seçildiğinde kütüphane/driver ve lisans sorumluluğu bu kuruluma aittir.

Varsayılan indirme CPU paketidir. GPU katmanları yalnızca açık geliştirici seçeneğiyle etkinleştirilir; başlangıç veya ilk token öncesindeki GPU hatası CPU ile yeniden denenebilir. Kısmi yanıt çıktıktan sonra otomatik tekrar yapılmaz. Driver'sız bir cihazda Vulkan/CUDA binary'sinin CPU fallback yapacağı varsayılmaz: kullanıcı varsayılan bağımsız CPU motorunu kurabilir.

## Uygulanan mimari ve dosyalar

| Katman | Başlıca dosyalar | Görev |
|---|---|---|
| Model/motor yönetimi | `internal/assistant/{catalog,service,receipt,extract,hardware*}.go`, `internal/database/migrations/009_assistant.sql` | Üç sabit model profili, mevcut download manager, SHA-256, güvenli atomik kurulum, etkin profil, kaldırma, donanım bilgisi |
| Inference | `internal/assistant/engine/{engine,process_*}.go` | İstek başına llama.cpp süreci, korumalı `/props` üzerinden kimlik kontrolü, tek yanıt, streaming, iptal/kapanış, CPU ve açık GPU seçeneği |
| Retrieval | `internal/assistant/rag/*.go`, `internal/providers/article.go`, `internal/providers/kiwix/article.go` | Kurulu ZIM araması/owned transport ile makale okuma, kurulu HTML metinleri, alaka filtresi, yerel kaynaklar ve sınırlandırılmış bağlam |
| API/yaşam döngüsü | `internal/api/assistant.go`, `internal/app/assistant*.go`, `internal/app/app.go` | İsteğe bağlı başlangıç, loopback/Origin koruması, bounded JSON ve POST SSE; bağımlılık hatası yalnız asistanı etkiler |
| Arayüz/dağıtım | `frontend/src/assistant.tsx`, `frontend/src/App.tsx`, sürüm/paketleme dosyaları ve `third_party/assistant-notices.md` | Mevcut terminal temasında model yönetimi, bellek içi sohbet, gerçek yerel alıntılar, durdurma; 1.0.0 kararlı sürümü |

Model dosyaları uygulamanın veri kökündeki `content/assistant/models/<profil>/model.gguf`, motor ve eşlik eden kütüphaneler `content/assistant/runtime` altında saklanır. Registry SQLite içindedir; indirmeler mevcut `downloads` mekanizmasını kullanır. Linux XDG ve Windows kullanıcı veri dizini kuralları değişmez. Kurulum sırasında doğrulama yapılır; uygulama açılışı ve status polling çok GB ağırlıkları yeniden hash'lemez, küçük receipt/kimlik/boyut kontrolleri yapar.

Retrieval sorgu başına en fazla beş ZIM paketinde, sınırlı iki arama turuyla çalışır; en fazla beş kaynak ve makale başına 128 KiB okuma kullanır. ZIM dosyaları kopyalanmaz veya her açılışta indekslenmez. Mevcut PDF içeriklerinin okunması çalışmaya devam eder fakat PDF metni AI retrieval kapsamına dahil değildir. HTML eğitim kaynaklarında yalnızca gerçekten ilgili metin kabul edilir; JavaScript/CSS çalıştırılmaz.

Kaynaklar model bağlamına JSON veri olarak girer; yalnızca bağlama sığan kaynaklar yanıta eşlik eder. Context, sistem talimatı, soru ve çıktı rezervi birlikte UTF-8 bayt üst sınırıyla konservatif sınırlandırılır. Çok uzun soru seçilen profilin bağlamına sığmazsa kullanıcıdan kısaltması istenir. Modelin yazdığı URL veya numara, tıklanabilir referansa dönüştürülmez.

## 1.0.0 doğrulama kapsamı

Asistan Engelsiz 1.0.0 kararlı sürümüne dahildir. Gerçek Linux CPU motorunun kurulumu doğrulandı; kullanıcı gerçek Lite modeliyle sohbetin çalıştığını doğruladı. Bu geri bildirim kontrollü kalite veya performans benchmark'ı değildir. Fiziksel 4-8 GB bellek, Windows AI inference ve gerçek GPU senaryoları ayrıca doğrulanmalıdır; sürüm etiketi bu testlerin yapılmış olduğu anlamına gelmez. [Komut sonuçları ve açık doğrulamalar](offline-ai-verification.md) · [1.0.0 sürüm notları](releases/1.0.0.md).
