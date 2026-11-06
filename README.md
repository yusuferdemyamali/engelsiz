<img src="frontend/public/logo.svg" alt="Engelsiz amblemi" width="72" height="72">

# Engelsiz

**Wikipedia, harita ve eğitim kaynaklarını indir; cihazında çevrimdışı kullan.**

An offline-first knowledge hub with Turkish Wikipedia, Türkiye maps, local translation and offline education.

**Türkçe** · [English](README.en.md)

[![CI](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml/badge.svg?branch=main)](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Windows için indir](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.5/Engelsiz-Setup.exe) · [Linux paketini indir](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.5/engelsiz-linux-x64.tar.gz) · [Tüm sürümler](https://github.com/yusuferdemyamali/engelsiz/releases) · [Hata bildir / öneri paylaş](https://github.com/yusuferdemyamali/engelsiz/issues/new/choose)

**Yayın durumu:** `v0.1.0-alpha.5` paketleri hazırdır ve Eğitim Merkezi ile Çeviri'yi içerir.

Engelsiz, seçtiğin bilgi kaynaklarını kendi bilgisayarında saklayan açık kaynaklı bir uygulamadır. İndirip kurduğun içerikler bağlantı olmadan açılır. Go uygulaması, yerel React arayüzünü varsayılan tarayıcında açar; ZIM arşivlerini paketlenen Kiwix runtime’ı sunar. PDF ve doğrulanmış HTML5 eğitim içerikleri de uygulama içinden okunur/çalıştırılır.

## Neler yapabilirsin?

- **Wikipedia:** Mini, Nopic ve Maxi arşivlerini seç; duraklatılabilir indirme, boyut/SHA-256 doğrulaması ve güvenli kurulum kullan. Seçeneklerin bulunabilirliği upstream kataloğa bağlıdır.
- **Ara ve oku:** Kurulu ZIM arşivinde çevrimdışı ara/oku; güncellemeleri kendin başlat. Yeni sürüm doğrulanana kadar önceki çalışan sürüm korunur.
- **Haritalar:** 81 ili ayrı ayrı indir ve bağlantısız aç. Yakınlaştırma, Türkçe etiketler ve kaynak gösterimi yereldir.
- **Çeviri:** İsteğe bağlı modellerle Türkçe ↔ İngilizce metin çevir; metinler cihazdaki Argos motorunda kalır.
- **Eğitim:** Afet ve İlk Yardım, Hayatta Kalma ve Pratik Bilgiler, Matematik ve Fen, İnteraktif Bilim Laboratuvarı kategorilerinden içerik keşfet, indir, aç ve kaldır. PDF, Türkçe Vikikitap ZIM'i ve üç Türkçe PhET simülasyonu desteklenir.

Arayüz ve font dosyaları uygulamaya dahildir. Uygulama yalnızca `127.0.0.1` üzerinde dinler; arayüzü açmak için bir hesap gerekmez.

## İndir ve çalıştır

| Platform | Durum |
| --- | --- |
| Linux x86_64 | [v0.1.0-alpha.5 ön sürümü hazır](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v0.1.0-alpha.5) |
| Windows x86_64 | [v0.1.0-alpha.5 kurulum paketini indir](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v0.1.0-alpha.5) |

Windows kurulum paketi için CI'da sessiz kurulum/çeviri/kaldırma kontrolleri geçti; Windows 10 henüz test edilmedi. Linux için kernel 4.4+ ve bir masaüstü tarayıcısı/opener gerekir. Go, Node, SQLite veya Kiwix’i ayrıca kurman gerekmez. Bu bir alpha yayınıdır; [tam platform kabul testleri](docs/release-validation.md) sürüyor.

### Windows

1. [Engelsiz-Setup.exe](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.5/Engelsiz-Setup.exe) ve [SHA-256 dosyasını](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.5/Engelsiz-Setup.exe.sha256) indir. PowerShell’de `Get-FileHash .\Engelsiz-Setup.exe -Algorithm SHA256` çıktısını SHA-256 dosyasındaki değerle karşılaştır.
2. Kurulum dosyasını çalıştır ve Başlat menüsünden Engelsiz’i aç. Yönetici izni veya ayrıca Go, Node, SQLite ya da Kiwix kurulumu gerekmez.
3. Bilgi Paketleri bölümünden bir arşiv indirip kur; ardından Ara bölümünden arama yap. Haritalar bölümünden il seçip harita indir; çevrimdışıyken Haritayı aç ile görüntüle. Kaldırma işlemi indirilen içerikleri ve ayarları korur.

### Linux

1. [Linux arşivini](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.5/engelsiz-linux-x64.tar.gz) ve [SHA-256 dosyasını](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.5/engelsiz-linux-x64.tar.gz.sha256) aynı klasöre indir.
2. O klasörde doğrula, arşivi aç ve uygulamayı başlat:

   ```sh
   sha256sum -c engelsiz-linux-x64.tar.gz.sha256
   tar -xzf engelsiz-linux-x64.tar.gz
   ./engelsiz/engelsiz
   ```

3. Açılan arayüzde **Bilgi Paketleri** bölümünden bir Wikipedia arşivi seçip indir ve kur. Kurulu içerikte **Ara** bölümünden arama yapabilirsin. **Haritalar** bölümünde il arayıp harita indirebilir, bağlantı kapalıyken görüntüleyebilirsin (il başına yaklaşık 7–80 MB).

Wikipedia verisi uygulama paketine dahil değildir. İlk içerik indirmesi için internet ve seçtiğin arşiv boyutuna uygun boş disk alanı gerekir. Paketler, Kiwix’in karşılık gelen kaynak kodlarını ve lisanslarını da içerir; Linux arşivi yaklaşık 426 MiB, Windows installer yaklaşık 277 MiB’dir.

Terminalde yazdırılan yerel adresi gerektiğinde elle açabilirsin. Varsayılan port `17321`’dir; doluysa uygulama uygun başka bir loopback portu seçer. Uygulamayı terminalde `Ctrl+C` ile kapatabilirsin.

## Verilerin nerede tutulur?

Linux’ta içerik ve veritabanı varsayılan olarak `~/.local/share/engelsiz`, ayarlar `~/.config/engelsiz`, önbellek `~/.cache/engelsiz` altındadır. `XDG_DATA_HOME`, `XDG_CONFIG_HOME` ve `XDG_CACHE_HOME` değişkenleri desteklenir. Arşivden çıkardığın program klasörünü silmek bu kullanıcı verilerini silmez.

## Offline Eğitim Merkezi

**Eğitim** sekmesinde 14 kayıt bulunur: 8 indirilebilir içerik ve 6 yalnızca kaynak bağlantısı. Başlangıç kaynakları üç WHO PDF'si, OpenStax Anatomy & Physiology, Türkçe Vikikitap ve üç Türkçe PhET simülasyonudur. Kaynak, dil, boyut, sürüm, lisans ve indirme durumu kartlarda gösterilir; büyük içeriklerin tamamını indirmen gerekmez.

1. İstediğin kategoriyi veya aramayı kullan; kaynağın dilini ve kullanım koşullarını incele.
2. **İndir** seç. Sayfadayken tamamlanan indirme doğrulanıp kurulur; gerekirse **Kur** ile devam et. İndirmeyi duraklatabilir veya yeniden deneyebilirsin.
3. **İçeriği aç** ile yerel PDF/ZIM/HTML5 okuyucusuna gir. Uygulamayı kapatıp interneti kestikten sonra aynı içerik yeniden açılır. **Sil**, yalnızca seçilen kaynağı kaldırır.

**Lisans ve kapsam:** WHO ve PhET materyalleri ticari olmayan kullanım koşullarına tabidir; Engelsiz'in MIT lisansı bunları değiştirmez. İzin veya uygulama içi görüntüleme uygunluğu doğrulanmayan AFAD, Sağlık Bakanlığı, MEB, iFixit ve CDC kayıtları yalnızca harici bağlantıdır; offline içerik değildir. Türkçe resmî afet/ilk yardım kapsamı henüz tam değildir. WHO KY+ bir stres yönetimi kursudur, ilk yardım kitabı değildir. Kolibri ayrı servis gerektirdiğinden entegre edilmemiştir.

[Gerçek kaynaklar, lisans kanıtları ve ağsız test raporu](docs/offline-education-center.md)

## Çevrimdışı çeviri

**Çeviri** modülü `main` dalında bulunur; paketlerin yayın durumu yukarıda belirtilir.

1. **Çeviri** bölümünü aç; kullanacağın yönün modelinde **İndir** seç. Türkçe → İngilizce yaklaşık 115 MiB, İngilizce → Türkçe yaklaşık 119 MiB'dir. Her yön bağımsızdır; indirme yalnızca sen başlatırsan yapılır.
2. İndirme bu sayfadayken tamamlanırsa kurulum başlar. Sayfadan ayrıldıysan geri dönüp **Kur** seç. İndirmeler bölümündeki duraklat/devam et/tekrar dene davranışları korunur.
3. Metni yaz veya yapıştır; **Çevir** ya da `Ctrl+Enter` kullan. **Yönü değiştir**, **Temizle** ve **Sonucu kopyala** kontrolleri aynı sayfadadır. Bir istek en fazla 5.000 Unicode karakter içerir.

Kurulumdan sonra bağlantı gerekmez. Metinler cihazdaki Argos motorunda işlenir; sunucuya gönderilmez, geçmişe veya loga yazılmaz. Python/pip kurulumu gerekmez: motor, yeni dağıtımın `runtime/translation` dizinine dahil edilir. Modeller normal kullanıcı veri dizinindeki `content/translation` altında saklanır. Ana uygulama, Kiwix ve PMTiles tamamen statiktir ve glibc gerektirmez; çevrimdışı çeviri motoru için glibc 2.38+ gerekir (Ubuntu 23.04+/Debian 13+/Fedora 38+ dönemi dağıtımlar). Windows native kabulü henüz yapılmadı. [Model kaynakları ve lisans sınırları](docs/translation-models.md) · [Teknik teslim ve test raporu](docs/offline-translation-delivery.md).

## Geliştirme ve katkı

Hata raporları, kurulum testleri, dokümantasyon, çeviri ve kod katkıları için [katkı rehberine](CONTRIBUTING.md) bakabilirsin. Yerel geliştirme komutları ve testler de bu rehberde bulunur.

- [Ürün kapsamı ve teknik gereksinimler](docs/main-spec.md)
- [Release doğrulaması ve platform kabul testleri](docs/release-validation.md)
- [Offline Eğitim Merkezi: kaynaklar, lisanslar ve testler](docs/offline-education-center.md)
- [Güvenli paketleme ve üçüncü taraf kaynakları](third_party/README.md)
- [Açık işler ve yol haritası](https://github.com/yusuferdemyamali/engelsiz/issues)
- [Uzun vadeli vizyon](docs/vision.md): daha fazla açık eğitim kaynağı ve yerel yapay zekâ; yapay zekâ soru-cevap özelliği henüz uygulanmamıştır.

## Lisans ve teşekkür

Engelsiz’in kendi kodu [MIT](LICENSE) lisanslıdır. Kiwix, bağımlılıkları ve indirilen içerikler kendi lisanslarına tabidir; dağıtılan runtime’ın kaynakları ve özgün notice dosyaları paket içindeki `LICENSES` klasöründedir. [Üçüncü taraf bilgileri](third_party/README.md).

Bu proje [Kiwix](https://kiwix.org/), [openZIM](https://openzim.org/) ve Wikipedia katkıcılarının çalışmalarından yararlanır.

> Bilginin erişim engeli olmaz.
