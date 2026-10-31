<img src="frontend/public/logo.svg" alt="Engelsiz amblemi" width="72" height="72">

# Engelsiz

**Türkçe Wikipedia’yı indir, cihazında ara ve internet olmadan oku.**

An offline-first Wikipedia reader for Turkish Wikipedia, powered by Kiwix and ZIM archives.

**Türkçe** · [English](README.en.md)

[![CI](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml/badge.svg?branch=main)](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Linux paketini indir](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.1/engelsiz-linux-x64.tar.gz) · [Tüm sürümler](https://github.com/yusuferdemyamali/engelsiz/releases) · [Hata bildir / öneri paylaş](https://github.com/yusuferdemyamali/engelsiz/issues/new/choose)

Engelsiz, Wikipedia arşivlerini kendi bilgisayarında saklayan açık kaynaklı bir uygulamadır. İlk içerik indirmesinden sonra arama ve makale okuma yerel olarak çalışır. Go ile yazılmış uygulama, React arayüzünü varsayılan tarayıcında açar; içerikleri pakete dahil edilen Kiwix runtime’ı sunar.

## Neler yapabilirsin?

- Türkçe Wikipedia’nın Mini, Nopic ve Maxi seçeneklerini katalogda inceleyebilirsin; seçeneklerin bulunabilirliği upstream kataloğa bağlıdır.
- İndirmeleri duraklatıp sürdürebilir, boyut ve SHA-256 doğrulamasından geçen ZIM dosyalarını kurabilirsin.
- Kurulu arşivde internet bağlantısı olmadan arama yapıp makale okuyabilirsin.
- Yeni sürümü görüp güncellemeyi kendin başlatabilirsin. Yeni içerik doğrulanıp etkinleşene kadar önceki çalışan sürüm korunur.

Arayüz ve font dosyaları uygulamaya dahildir. Uygulama yalnızca `127.0.0.1` üzerinde dinler; arayüzü açmak için bir hesap gerekmez.

## İndir ve çalıştır

| Platform | Durum |
| --- | --- |
| Linux x86_64 | [v0.1.0-alpha.1 ön sürümü hazır](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v0.1.0-alpha.1) |
| Windows x86_64 | [Kurulum paketi geliştiriliyor](https://github.com/yusuferdemyamali/engelsiz/issues/10); henüz yayımlanmış installer yok |

Linux için kernel 4.4+ ve bir masaüstü tarayıcısı/opener gerekir. Go, Node, SQLite veya Kiwix’i ayrıca kurman gerekmez. Bu bir alpha yayınıdır; [tam platform kabul testleri](docs/release-validation.md) sürüyor.

1. [Linux arşivini](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.1/engelsiz-linux-x64.tar.gz) ve [SHA-256 dosyasını](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.1/engelsiz-linux-x64.tar.gz.sha256) aynı klasöre indir.
2. O klasörde doğrula, arşivi aç ve uygulamayı başlat:

   ```sh
   sha256sum -c engelsiz-linux-x64.tar.gz.sha256
   tar -xzf engelsiz-linux-x64.tar.gz
   ./engelsiz/engelsiz
   ```

3. Açılan arayüzde **Bilgi Paketleri** bölümünden bir Wikipedia arşivi seçip indir ve kur. Kurulu içerikte **Ara** bölümünden arama yapabilirsin.

Wikipedia verisi uygulama paketine dahil değildir. İlk içerik indirmesi için internet ve seçtiğin arşiv boyutuna uygun boş disk alanı gerekir. Paket, Kiwix’in karşılık gelen kaynak kodlarını ve lisanslarını da içerdiği için yaklaşık 295 MiB’dir.

Terminalde yazdırılan yerel adresi gerektiğinde elle açabilirsin. Varsayılan port `17321`’dir; doluysa uygulama uygun başka bir loopback portu seçer. Uygulamayı terminalde `Ctrl+C` ile kapatabilirsin.

## Verilerin nerede tutulur?

Linux’ta içerik ve veritabanı varsayılan olarak `~/.local/share/engelsiz`, ayarlar `~/.config/engelsiz`, önbellek `~/.cache/engelsiz` altındadır. `XDG_DATA_HOME`, `XDG_CONFIG_HOME` ve `XDG_CACHE_HOME` değişkenleri desteklenir. Arşivden çıkardığın program klasörünü silmek bu kullanıcı verilerini silmez.

## Geliştirme ve katkı

Hata raporları, kurulum testleri, dokümantasyon, çeviri ve kod katkıları için [katkı rehberine](CONTRIBUTING.md) bakabilirsin. Yerel geliştirme komutları ve testler de bu rehberde bulunur.

- [Ürün kapsamı ve teknik gereksinimler](docs/main-spec.md)
- [Release doğrulaması ve platform kabul testleri](docs/release-validation.md)
- [Güvenli paketleme ve üçüncü taraf kaynakları](third_party/README.md)
- [Açık işler ve yol haritası](https://github.com/yusuferdemyamali/engelsiz/issues)
- [Uzun vadeli vizyon](docs/vision.md): farklı açık bilgi kaynakları, haritalar, eğitim ve yerel yapay zekâ; bunlar mevcut alpha sürümünün özellikleri değildir.

## Lisans ve teşekkür

Engelsiz’in kendi kodu [MIT](LICENSE) lisanslıdır. Kiwix, bağımlılıkları ve indirilen içerikler kendi lisanslarına tabidir; dağıtılan runtime’ın kaynakları ve özgün notice dosyaları paket içindeki `LICENSES` klasöründedir. [Üçüncü taraf bilgileri](third_party/README.md).

Bu proje [Kiwix](https://kiwix.org/), [openZIM](https://openzim.org/) ve Wikipedia katkıcılarının çalışmalarından yararlanır.

> Bilginin erişim engeli olmaz.
