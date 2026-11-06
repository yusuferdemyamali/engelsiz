# Engelsiz 1.0.0 ürün ve mimari spesifikasyonu

Sürüm kanalı: **kararlı**. Hedef dağıtımlar: Windows x86_64 ve Linux x86_64. [Kurulum](../README.md) · [Sürüm notları](releases/1.0.0.md) · [Doğrulama kapsamı](release-validation.md).

## Amaç ve ilkeler

Engelsiz, önceden indirilen bilgi kaynaklarını internet bağlantısı olmadan kullanmayı sağlayan local-first bir Go + React/TypeScript uygulamasıdır. Arayüz ve fontlar yereldir; Go uygulaması yalnızca loopback üzerinde dinler ve varsayılan tarayıcıyı açar. Hesap, merkezi sunucu veya bulut inference gerekmez.

1. Kurulu içeriklerin açılması, aranması ve kullanılması çevrimdışıdır. Katalog, kullanıcı tarafından başlatılan indirmeler, harita çıkarma ve güncelleme kontrolleri internet gerektirebilir.
2. İçerik ve model indirmeleri açık kullanıcı eylemiyle başlar; büyük paketler veya AI otomatik kurulmaz.
3. Kaynaklar mevcut Kiwix/ZIM, PMTiles, Argos ve llama.cpp altyapılarıyla kullanılır; özel Wikipedia parser'ı, tam yeniden indeksleme veya yeni bulut servisi eklenmez.
4. AI ve diğer isteğe bağlı motorların kullanılamaması Wikipedia, harita ve indirme altyapısının açılmasını engellemez.
5. İndirmeler boyut/hash doğrulaması, güvenli dosya sahipliği ve kurulum sınırlarıyla yönetilir. Kaynakların kendi lisans ve attribution koşulları korunur.

## 1.0.0 kapsamı

| Modül | İşlev |
|---|---|
| Bilgi Paketleri / Yerel Paketler | Türkçe Wikipedia Mini/Nopic/Maxi kataloğu, indirme, doğrulama, kurulum ve kullanıcı kontrollü güncelleme |
| İndirmeler | SQLite tabanlı kalıcı durum, ilerleme, duraklatma/devam, retry, iptal, alan kontrolü |
| Ara / oku | Kurulu ZIM'lerde Kiwix araması ve yerel makale görüntüleme |
| Haritalar | 81 il için ayrı indirilebilen PMTiles, yerel Türkçe etiket/glyph/sprite, çevrimdışı görüntüleme |
| Çeviri | Ayrı, isteğe bağlı Türkçe-İngilizce ve İngilizce-Türkçe Argos modelleri; paketli yerel worker |
| Eğitim | PDF/ZIM/HTML5 kaynakları, Türkçe Vikikitap ve üç Türkçe PhET simülasyonu; lisans ve kaynak bilgisi |
| Asistan | İsteğe bağlı yerel Qwen3 GGUF modelleri, llama.cpp CPU inference, kurulu metin kaynaklarından RAG ve gerçek yerel kaynak bağlantıları |

### Yerel AI

Lite: Qwen3 0.6B Q4_K_M, 4 GB RAM hedefi. Balanced: Qwen3 1.7B Q4_K_M, 8 GB hedefi. Quality: Qwen3 4B Q4_K_M, 12-16 GB hedefi. Bunlar uyumluluk garantisi değil başlangıç hedefleridir. Lite/Balanced Unsloth topluluk quantization, Quality resmi Qwen dağıtımıdır; metadata, lisans, revision ve SHA-256 [asistan belgesinde](offline-ai.md) sabittir.

Motor yalnızca ihtiyaç halinde başlar. Tek inference lease'i eşzamanlı yanıtları ve çakışan model değişikliklerini engeller. Yanıt tamamlanınca, iptal edilince veya uygulama kapanınca süreç temizlenir. Varsayılan bağımsız CPU paketidir; GPU yalnızca açık seçenekle ve ilk token öncesi CPU retry sınırıyla kullanılabilir.

Retrieval kurulu Kiwix ZIM'lerinde ve desteklenen kurulu HTML metinlerinde sınırlı arama/okuma yapar. Her açılışta yeniden indeksleme yoktur. Kaynak bulunamazsa deterministik yetersiz bilgi yanıtı verilir. Referanslar modelin metninden değil gerçek retrieval sonuçlarından üretilir. Kaynaklar güvenilmeyen JSON verisi olarak bağlama girer; araç veya kod çalıştırma yetkisi yoktur. PDF metin çıkarımı AI kapsamına dahil değildir.

Sohbet POST SSE ile streaming çalışır; kullanıcı durdurabilir. Her soru bağımsız model bağlamıdır. Görünen geçmiş sadece bellekte tutulur ve ekrandan ayrılınca silinir. Soru, kaynak ve yanıtlar dış sunucuya veya loga gönderilmez. Afet/sağlık konularında üretilen yanıtın doğrulanmış kaynak metni olmadığı açıkça belirtilir.

## Mimari sınırlar

- Backend: Go `net/http`, yerel süreç yönetimi, SQLite metadata/state; production frontend Go `embed` ile sunulur.
- Frontend: React/TypeScript/Vite, mevcut terminal teması ve Tailwind bileşenleri; harita ve AI ekranları ayrı yüklenir.
- ZIM içeriği SQLite'a kopyalanmaz. Kiwix süreçleri ve transport'ları sahip olunan yerel runtime generation'ına bağlıdır.
- AI yönetimi `internal/assistant`, inference `internal/assistant/engine`, retrieval `internal/assistant/rag` içindedir. API/app katmanı modülleri birleştirir.
- Linux XDG ve Windows kullanıcı veri dizinleri kullanılır. Programı kaldırmak kullanıcı içeriklerini kendiliğinden silmez. Model/engine kurulumları kullanıcı veri kökündedir.

## Dağıtım ve doğrulama

Son kullanıcı Go/Node/Python/SQLite/Kiwix kurmaz. Kiwix/PMTiles ve çeviri runtime'ı platform paketine dahildir. AI engine ve modelleri açık kullanıcı eylemiyle ayrı indirilir. Linux çekirdek uygulaması statiktir; hazır AI motoru glibc 2.35+, çeviri motoru glibc 2.38+ gerektirir. Windows motorlarında VC++ x64 runtime önkoşulu olabilir.

Paketleme hash-pinned runtime/source/compliance ve lisans envanterlerini doğrular; eksik/bozuk girdilerde durur. Kararlı yayın, iki native platform build'i ve yeni installer üzerinde Windows QA başarılı olduktan sonra, aynı test edilmiş commit'e bağlanan `v1.0.0` etiketiyle yapılır. SHA-256 ve build provenance sürüm varlıklarına eklenir. Fiziksel düşük RAM, Windows 10 ve GPU kapsamındaki yapılmamış testler tamamlanmış gibi gösterilmez.

## Kapsam dışı

Bulut AI, hesap/cloud sync, agent tool calling veya bilgisayar otomasyonu, web araştırması, fine-tuning, sesli asistan, görsel üretimi, AI harita otomasyonu ve özel belge yüklemeli ayrı RAG yoktur. Mobil/macOS/ARM dağıtımı, Kolibri servisi, plugin marketplace ve otomatik büyük içerik güncellemeleri mevcut sürümün parçası değildir.

## Ayrıntılı belgeler

- [AI mimarisi, kurulum ve sabit kaynaklar](offline-ai.md) / [AI test kanıtı](offline-ai-verification.md)
- [Eğitim kaynakları ve doğrulama](offline-education-center.md)
- [Çeviri modelleri](translation-models.md) / [çeviri teslim raporu](offline-translation-delivery.md)
- [Paketleme ve platform test prosedürleri](release-validation.md)
- [İlk v0.1 temel mimari kararlarının tarihsel arşivi](archive/v0.1-foundation-spec.md)
