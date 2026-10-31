# Engelsiz — Main Specification

> Sürüm: v0.1 teknik ürün spesifikasyonu\
> Durum: İlk uygulama için onaylanmış ana kararlar\
> Hedef platformlar: Windows + Linux

---

## 1. Projenin Amacı

**[[Engelsiz]]**, internet bağlantısı olmasa bile temel bilgi kaynaklarına yerel olarak erişilebilmesini sağlayan, kurulumu kolay, local-first bir masaüstü/localhost uygulamasıdır.

Uygulamanın amacı internet bağlantısının yerini almak değildir. Amaç; önceden indirilen açık bilgi kaynaklarının internet olmadan da aranabilmesi, okunabilmesi ve ileride farklı offline bilgi sağlayıcılarının tek bir arayüz üzerinden yönetilebilmesidir.

İlk sürüm yalnızca **Kiwix üzerinden Türkçe Wikipedia** desteği sunacaktır.

Engelsiz'in uzun vadeli yönü yalnızca Wikipedia değildir. Mimari ileride farklı içerik sağlayıcılarının eklenmesini mümkün kılmalıdır; ancak v0.1 sırasında bu gelecek senaryoları için gereksiz implementasyon veya aşırı soyutlama yapılmamalıdır.

---

## 2. Temel Ürün İlkeleri

### 2.1 Offline-first

İndirilmiş içeriklerin:

- aranması,
- açılması,
- okunması,
- listelenmesi

için internet bağlantısı gerekmemelidir.

İnternet yalnızca:

- katalog yenileme,
- yeni içerik indirme,
- güncelleme kontrolü,
- içerik güncelleme

işlemleri için gereklidir.

### 2.2 Local-first

Kullanıcı verileri ve indirilen bilgi paketleri kullanıcının kendi cihazında tutulmalıdır.

Engelsiz'in çalışması için merkezi bir Engelsiz hesabı, Engelsiz sunucusu veya cloud backend zorunlu olmamalıdır.

### 2.3 Kolay kurulum

Kullanıcıdan aşağıdaki yazılımları ayrıca kurması istenmemelidir:

- Go
- Node.js
- npm
- SQLite
- Kiwix
- `kiwix-serve`
- Docker
- PHP
- Python
- harici database server

Gerekli runtime bileşenleri release paketine dahil edilmelidir.

### 2.4 Tekerleği yeniden icat etmeme

Kiwix'in zaten çözdüğü problemler Engelsiz içinde yeniden implemente edilmemelidir.

Özellikle v0.1'de Engelsiz:

- Wikipedia dump üretmeyecek,
- Wikipedia XML parse etmeyecek,
- ZIM formatını yeniden implemente etmeyecek,
- Wikipedia için özel full-text search motoru yazmayacak,
- Wikipedia içeriğini SQLite'a dönüştürmeyecek.

Kiwix'in hazır **ZIM** ekosistemi ve `kiwix-serve` kullanılacaktır.

### 2.5 Geleceğe açık ama bugüne odaklı

Core mimari yalnızca Kiwix'e kilitlenmemelidir.

Ancak v0.1 içinde kullanılmayan:

- MapsProvider,
- KolibriProvider,
- EPUB provider,
- local AI provider,
- plugin marketplace

gibi özellikler implement edilmemelidir.

Mimari bunların ileride eklenmesini engellemeyecek kadar modüler olmalıdır.

---

# 3. v0.1 Kapsamı

v0.1'in amacı tek bir dikey akışı eksiksiz çalıştırmaktır:

> **Kiwix kataloğunu getir → Türkçe Wikipedia seçeneklerini göster → kullanıcı bir sürüm seçsin → indir → doğrula → kur → offline çalıştır → ara → oku.**

## 3.1 Dahil olan özellikler

v0.1 aşağıdakileri içerecektir:

- Windows desteği
- Linux desteği
- localhost tabanlı web arayüzü
- Go core
- React + TypeScript frontend
- SQLite uygulama veritabanı
- Kiwix katalog entegrasyonu
- yalnızca Türkçe Wikipedia
- Wikipedia `mini`
- Wikipedia `nopic`
- Wikipedia `maxi`
- ZIM indirme
- indirme ilerlemesi
- indirme devam ettirme / resume
- geçici `.part` dosyası
- retry
- disk alanı kontrolü
- checksum doğrulaması
- kurulu paketlerin listelenmesi
- kurulu sürüm bilgisi
- yeni sürüm kontrolü
- kullanıcı onayıyla güncelleme
- eski sürümü yeni sürüm doğrulanana kadar koruma
- `kiwix-serve` lifecycle yönetimi
- Kiwix araması
- Wikipedia makale görüntüleme
- browser'ın otomatik açılması
- temiz uygulama kapanışı
- temel loglama
- temel hata ekranları

## 3.2 v0.1 dışında kalanlar

Aşağıdakiler v0.1 kapsamında değildir:

- local LLM
- RAG
- embeddings
- vector database
- offline haritalar
- OpenStreetMap / PMTiles
- Kolibri
- Gutenberg
- WikiHow için özel entegrasyon
- EPUB reader
- PDF library
- mobil uygulama
- Android
- iOS
- macOS
- LAN üzerinden paylaşım
- çok kullanıcılı kullanım
- remote access
- kullanıcı hesabı
- cloud sync
- torrent download
- mirror switching
- background auto-update
- otomatik büyük ZIM güncellemesi
- özel Wikipedia parser
- özel Wikipedia arama indeksi

Bu maddeler için v0.1 sırasında placeholder ekran veya yarım implementasyon eklenmemelidir.

---

# 4. Teknoloji Stack'i

## 4.1 Backend / Core

- **Go**
- Go standart `net/http`
- Go `embed`
- process lifecycle yönetimi
- reverse proxy
- download manager
- SQLite erişimi

Backend ayrı bir harici web server gerektirmemelidir.

## 4.2 Frontend

- **React**
- **TypeScript**
- **Vite**
- Tailwind CSS
- shadcn/ui

Frontend production build'i Go binary'si tarafından serve edilmelidir.

## 4.3 Uygulama veritabanı

- **SQLite**

SQLite yalnızca Engelsiz'e ait metadata/state için kullanılacaktır.

Örnek:

- settings
- installed content
- download state
- provider metadata
- installed versions
- timestamps

Wikipedia makaleleri SQLite'a import edilmeyecektir.

## 4.4 Offline içerik runtime'ı

- **Kiwix**
- `kiwix-serve`
- `.zim`

Kullanıcıdan ayrıca Kiwix kurulumu istenmemelidir.

---

# 5. Genel Mimari

```text
┌─────────────────────────────────────────────────────────┐
│                        Engelsiz                         │
│                                                         │
│  ┌───────────────────┐        ┌──────────────────────┐  │
│  │    React UI       │        │       Go Core        │  │
│  │                   │◄──────►│                      │  │
│  │ Browser UI        │ HTTP   │ API                  │  │
│  └───────────────────┘        │ Package Registry     │  │
│                               │ Download Manager     │  │
│                               │ Provider Layer       │  │
│                               │ Runtime Manager      │  │
│                               └──────────┬───────────┘  │
│                                          │              │
│                           ┌──────────────┴────────────┐ │
│                           │      Kiwix Provider       │ │
│                           └──────────────┬────────────┘ │
└──────────────────────────────────────────┼──────────────┘
                                           │
                                           ▼
                                  ┌──────────────────┐
                                  │   kiwix-serve    │
                                  │   127.0.0.1      │
                                  └────────┬─────────┘
                                           │
                                           ▼
                                  ┌──────────────────┐
                                  │    .zim files    │
                                  │ Turkish Wikipedia│
                                  └──────────────────┘
```

---

# 6. Uygulama Çalışma Modeli

Engelsiz klasik desktop WebView uygulaması olmayacaktır.

Ana model:

```text
Engelsiz executable
        ↓
Go core başlatılır
        ↓
local HTTP server başlatılır
        ↓
gerekiyorsa kiwix-serve başlatılır
        ↓
React UI serve edilir
        ↓
varsayılan browser açılır
        ↓
http://127.0.0.1:<port>
```

## 6.1 Network binding

Engelsiz varsayılan olarak sadece:

```text
127.0.0.1
```

üzerinde dinlemelidir.

Varsayılan olarak:

```text
0.0.0.0
```

üzerinde dinlememelidir.

Bu nedenle aynı LAN üzerindeki diğer cihazlar Engelsiz'e erişememelidir.

LAN paylaşımı gelecekte ayrı ve açıkça etkinleştirilen bir özellik olabilir.

## 6.2 Port seçimi

Tercih edilen Engelsiz portu:

```text
17321
```

olabilir.

Port doluysa uygulama:

1. hata verip tamamen kapanmamalı,
2. kullanılabilir başka bir local port bulmalı,
3. browser'ı gerçek kullanılan port ile açmalıdır.

Kiwix için ayrı bir internal loopback portu seçilebilir.

Internal Kiwix portu kullanıcı arayüzünde gösterilmemelidir.

---

# 7. Process Lifecycle

## 7.1 Startup

Uygulama açılırken sırasıyla:

1. uygulama dizinleri hazırlanır,
2. config okunur,
3. SQLite açılır / migration uygulanır,
4. yarım kalan download state'i kontrol edilir,
5. kurulu Kiwix içerikleri bulunur,
6. gerekiyorsa `kiwix-serve` başlatılır,
7. Engelsiz HTTP server başlatılır,
8. health check yapılır,
9. browser açılır.

Browser, backend hazır olmadan açılmamalıdır.

## 7.2 Kiwix process yönetimi

`kiwix-serve` Engelsiz tarafından child process olarak yönetilecektir.

Engelsiz:

- process'i başlatmalı,
- PID/state takip etmeli,
- beklenmedik kapanmayı algılamalı,
- gerektiğinde kontrollü restart edebilmeli,
- uygulama kapanışında child process'i sonlandırmalıdır.

Aynı Engelsiz instance'ı aynı runtime için birden fazla `kiwix-serve` instance oluşturmamalıdır.

## 7.3 Shutdown

Uygulama kapanırken:

1. aktif download state'i güvenli biçimde kaydedilir,
2. açık stream/file handle'lar kapatılır,
3. `kiwix-serve` sonlandırılır,
4. SQLite bağlantısı kapatılır,
5. Engelsiz process'i sonlanır.

Yarım kalan download silinmemelidir.

---

# 8. Content Provider Mimarisi

v0.1'de yalnızca Kiwix implement edilecektir.

Ancak core'un content provider kavramından haberdar olması tercih edilir.

Amaç:

```text
ContentProvider
      │
      └── KiwixProvider   ← v0.1'de tek gerçek provider
```

Gelecekte:

```text
ContentProvider
├── KiwixProvider
├── MapsProvider
├── EducationProvider
└── ...
```

gibi genişleyebilmesidir.

## 8.1 Provider sorumlulukları

Provider genel olarak şu yeteneklerin bazılarını sunabilir:

- katalog listeleme
- paket metadata çözümleme
- indirme kaynağını belirleme
- paket kurma
- paket kaldırma
- versiyon karşılaştırma
- içerik runtime'ını başlatma
- search desteği
- content open/read desteği

Her provider'ın bütün yetenekleri desteklemesi zorunlu değildir.

## 8.2 Over-engineering yasağı

v0.1 sırasında yalnızca Kiwix mevcut olduğu için:

- dinamik plugin loading,
- reflection tabanlı plugin sistemi,
- harici plugin SDK,
- ayrı process provider host,
- marketplace

oluşturulmayacaktır.

Basit Go interface + concrete Kiwix implementation yeterlidir.

---

# 9. Kiwix Entegrasyonu

## 9.1 Kaynak

Wikipedia paketleri Kiwix'in canlı kataloğundan bulunmalıdır.

Tarih içeren ZIM URL'leri source code içine sabitlenmemelidir.

Örnek olarak şu yaklaşım kullanılmamalıdır:

```text
wikipedia_tr_all_maxi_2026-05.zim
```

gibi tarihli bir dosyanın URL'sini kalıcı sabit olarak tutmak.

Bunun yerine uygulama katalog metadata'sından güncel uygun ZIM kaynağını çözümlemelidir.

## 9.2 Filtre

v0.1 yalnızca:

- dil: Türkçe
- içerik: Wikipedia

sonuçlarını göstermelidir.

Kullanıcının genel Kiwix kataloğunda yüzlerce içerik arasında gezinmesi v0.1 kapsamı değildir.

## 9.3 Wikipedia varyantları

UI aşağıdaki üç temel seçeneği desteklemelidir:

### Mini

- en düşük disk kullanımı
- sadeleştirilmiş içerik
- düşük depolama alanına sahip cihazlar için

### Nopic

- daha geniş/tam metin
- görselsiz
- disk alanı ile içerik kapsamı arasında denge

### Maxi

- en kapsamlı sürüm
- görseller dahil
- en yüksek disk kullanımı

Kullanıcıya katalogdan alınabilen gerçek:

- boyut,
- yayın tarihi / sürüm,
- açıklama

gösterilmelidir.

Boyutlar source code içine sabit yazılmamalıdır.

---

# 10. Katalog Akışı

Kullanıcı "Bilgi Paketleri" ekranını açtığında:

```text
UI
 ↓
Engelsiz API
 ↓
KiwixProvider
 ↓
Kiwix catalog
 ↓
normalize edilmiş metadata
 ↓
UI
```

Frontend doğrudan Kiwix'e bağlanmamalıdır.

Tüm provider iletişimi Go core üzerinden geçmelidir.

## 10.1 Offline durumda

Katalog endpoint'i erişilemiyorsa:

- kurulu Wikipedia çalışmaya devam etmelidir,
- uygulama ana ekranı çalışmaya devam etmelidir,
- anlaşılır bir "Katalog şu anda yenilenemiyor" durumu gösterilmelidir.

Bu durum uygulamanın tamamını hata ekranına düşürmemelidir.

---

# 11. Download Manager

Download Manager provider'dan mümkün olduğunca bağımsız olmalıdır.

KiwixProvider uygun download metadata'sını belirler; Download Manager transferi gerçekleştirir.

## 11.1 Gerekli özellikler

v0.1 Download Manager:

- HTTP/HTTPS download
- streaming write
- `.part` dosyası
- resume
- HTTP Range
- progress
- downloaded bytes
- total bytes
- yüzde
- retry
- cancel
- disk-space pre-check
- checksum doğrulama
- atomic finalize

desteklemelidir.

## 11.2 Dosya akışı

Örnek:

```text
wikipedia_tr_all_nopic.zim.part
                 ↓
download tamamlandı
                 ↓
checksum
                 ↓
başarılı
                 ↓
atomic rename
                 ↓
wikipedia_tr_all_nopic.zim
```

Checksum başarısız olursa `.part` veya bozuk sonuç doğrudan kurulu paket olarak işaretlenmemelidir.

## 11.3 Resume

Uygulama veya bilgisayar download sırasında kapanırsa:

- indirilmiş byte'lar korunmalıdır,
- uygulama yeniden açıldığında kullanıcı devam edebilmelidir,
- download zorunlu olarak sıfırdan başlamamalıdır.

Server Range desteklemiyorsa güvenli fallback uygulanabilir.

## 11.4 Disk alanı

Download başlamadan önce:

- hedef dosyanın beklenen boyutu bilinmeye çalışılmalı,
- hedef diskte yeterli alan kontrol edilmeli,
- geçici `.part` dosyası için gereken alan hesaba katılmalıdır.

Yeterli alan yoksa download başlamamalıdır.

---

# 12. Güncelleme Sistemi

ZIM güncellemesi incremental patch olarak varsayılmamalıdır.

Yeni upstream ZIM bulunduğunda kullanıcıya gösterilmelidir.

Örnek UI:

```text
Türkçe Wikipedia

Kurulu:
Mayıs 2026

Yeni sürüm:
Ağustos 2026

Boyut:
9.0 GB

[Güncelle]
```

## 12.1 Otomatik indirme yok

Büyük ZIM dosyaları kullanıcı izni olmadan otomatik indirilmeyecektir.

Uygulama yalnızca:

- güncelleme olduğunu gösterebilir,
- kullanıcının açık aksiyonuyla download başlatabilir.

## 12.2 Güvenli update

Güncelleme sırasında:

```text
old.zim          ← kullanılabilir durumda kalır

new.zim.part
      ↓
download
      ↓
checksum
      ↓
new.zim
      ↓
runtime refresh
      ↓
başarı
      ↓
old.zim kaldırılabilir
```

Yeni dosya tamamen doğrulanmadan eski çalışan ZIM silinmemelidir.

---

# 13. Kiwix Serving

Kiwix içerikleri `kiwix-serve` ile açılacaktır.

Engelsiz kullanıcıyı doğrudan ayrı bir Kiwix portuna göndermemelidir.

Tercih edilen model:

```text
Browser
   │
   ▼
127.0.0.1:<engelsiz-port>
   │
   ├── /api/*       → Go API
   ├── /assets/*    → Engelsiz frontend
   └── /content/*   → internal reverse proxy → kiwix-serve
```

Bu sayede kullanıcı tek origin/tek localhost deneyimi görür.

---

# 14. Search

v0.1 için Wikipedia araması Kiwix'in mevcut search kabiliyetinden yararlanmalıdır.

Engelsiz kendi Wikipedia FTS indeksini üretmemelidir.

Akış:

```text
Search input
   ↓
React
   ↓
Engelsiz Go API
   ↓
KiwixProvider
   ↓
kiwix-serve search
   ↓
normalize sonuç
   ↓
React
```

## 14.1 Search sonucu

Mümkün olduğu ölçüde:

- başlık
- kısa snippet
- içerik kaynağı
- makale link/id

döndürülmelidir.

---

# 15. Makale Görüntüleme

v0.1'de öncelik çalışan ve güvenilir offline okuyucudur.

Engelsiz ilk sürümde Kiwix'in render edilmiş içeriğini kullanabilir.

Kendi özel Wikipedia renderer'ı v0.1 kapsamında değildir.

Engelsiz wrapper/header/navigation sunabilir; ancak Kiwix'in çalışan içerik render mekanizmasını gereksiz yere yeniden yazmamalıdır.

---

# 16. Uygulama Verisi

## 16.1 SQLite

Önerilen ana tablolar:

### `settings`

```text
key
value
updated_at
```

### `providers`

```text
id
type
enabled
created_at
updated_at
```

v0.1'de tek provider Kiwix olabilir.

### `content_packages`

```text
id
provider
provider_package_id
content_type
language
variant
title
version
source_url
local_path
size_bytes
checksum
status
installed_at
updated_at
```

### `downloads`

```text
id
provider
package_id
source_url
destination_path
temp_path
total_bytes
downloaded_bytes
status
checksum
error
created_at
updated_at
```

Kesin SQL schema uygulama sırasında migration'larla tanımlanacaktır.

## 16.2 Status örnekleri

Package:

```text
not_installed
downloading
verifying
installed
update_available
error
```

Download:

```text
queued
downloading
paused
verifying
completed
cancelled
failed
```

State machine gereksiz karmaşıklığa dönüştürülmemeli; ancak geçersiz kombinasyonlardan kaçınılmalıdır.

---

# 17. Dosya Sistemi

İçerikler executable'ın yanına rastgele yazılmamalıdır.

OS standartlarına uygun user data dizinleri kullanılmalıdır.

Mantıksal yapı:

```text
EngelsizData/
├── engelsiz.db
├── config/
├── logs/
├── downloads/
│   └── *.part
├── content/
│   └── kiwix/
│       └── *.zim
└── runtime/
```

## 17.1 Windows

Uygulama verileri uygun Windows user app-data dizininde tutulmalıdır.

Büyük ZIM içerikleri için ileride kullanıcıya farklı storage location seçme özelliği eklenebilir.

v0.1'de varsayılan konum kullanılabilir.

## 17.2 Linux

XDG dizinleri tercih edilmelidir.

Örneğin uygun şekilde:

- config için XDG config
- state/db/content için XDG data
- cache/geçici dosyalar için uygun cache/data dizini

kullanılmalıdır.

---

# 18. REST API

API v0.1 için basit tutulmalıdır.

Örnek endpoint grupları:

```text
GET    /api/health

GET    /api/packages
GET    /api/catalog
POST   /api/packages/:id/install
POST   /api/packages/:id/update
DELETE /api/packages/:id

GET    /api/downloads
POST   /api/downloads/:id/pause
POST   /api/downloads/:id/resume
POST   /api/downloads/:id/cancel

GET    /api/search?q=...

GET    /api/settings
PATCH  /api/settings
```

Bu isimler kesin public API sözleşmesi olarak değerlendirilmemelidir; implementasyon sırasında mantıklı REST yapısı korunarak değiştirilebilir.

Frontend provider-specific internal detayları bilmemelidir.

---

# 19. UI / Ana Ekranlar

v0.1 için minimum ekranlar:

## 19.1 Ana Sayfa

Gösterilecek temel bilgiler:

- Engelsiz durumu
- kurulu bilgi paketleri
- hızlı arama
- varsa güncelleme bildirimi
- aktif download

## 19.2 Bilgi Paketleri

Türkçe Wikipedia seçenekleri:

```text
Türkçe Wikipedia

[Mini]
Boyut: ...
Sürüm: ...
[İndir]

[Nopic]
Boyut: ...
Sürüm: ...
[İndir]

[Maxi]
Boyut: ...
Sürüm: ...
[İndir]
```

Kurulu varyant açıkça gösterilmelidir.

## 19.3 Download ekranı

- dosya adı
- varyant
- progress
- downloaded / total
- status
- pause/resume
- cancel
- hata varsa anlaşılır mesaj

## 19.4 Arama

- search input
- sonuç listesi
- boş durum
- içerik bulunamadı durumu
- offline çalışabilme

## 19.5 Makale

- Kiwix üzerinden render edilmiş içerik
- Engelsiz navigation'a geri dönüş

## 19.6 Ayarlar

v0.1'de minimum düzeyde tutulmalıdır.

Gereksiz ayar kalabalığı oluşturulmamalıdır.

---

# 20. Hata Yönetimi

Kullanıcıya mümkün olduğunca teknik stack trace gösterilmemelidir.

Ancak loglarda teknik detay bulunmalıdır.

Önemli hata senaryoları:

### İnternet yok

Davranış:

- kurulu içerik çalışır,
- katalog yenileme başarısız olarak gösterilir,
- uygulama kullanılabilir kalır.

### Kiwix catalog erişilemiyor

- mevcut kurulu içerik bozulmaz,
- retry sunulur.

### Disk dolu

- mümkünse download başlamadan engellenir,
- yarım download silinmez.

### Download kesildi

- `.part` korunur,
- resume sunulur.

### Checksum hatası

- paket installed olarak işaretlenmez,
- kullanıcıya doğrulama hatası gösterilir.

### `kiwix-serve` başlatılamadı

- uygulama tamamen sessizce kapanmamalıdır,
- kullanıcıya içerik runtime hatası gösterilmelidir,
- loglara stderr/stdout kaydedilmelidir.

### Port kullanımda

- alternatif port denenir.

### SQLite bozuk / migration hatası

- veri kaybına yol açacak otomatik reset yapılmamalıdır,
- açık hata kaydı üretilmelidir.

---

# 21. Logging

Uygulama dosya tabanlı temel log üretmelidir.

Minimum:

- startup
- shutdown
- selected ports
- Kiwix process start/stop
- catalog errors
- download errors
- checksum errors
- database migration errors

Loglarda:

- gereksiz kullanıcı içeriği,
- hassas veri,
- devasa response body

tutulmamalıdır.

Log rotation basit tutulabilir.

---

# 22. Güvenlik

## 22.1 Localhost

Tüm servisler varsayılan olarak loopback'e bind edilmelidir.

## 22.2 Path traversal

Provider veya katalogdan gelen dosya isimleri doğrudan güvenilir kabul edilmemelidir.

Download target ve extraction/path işlemleri canonical path kontrolü kullanmalıdır.

## 22.3 Checksum

Mümkün olan Kiwix artefact'larında checksum doğrulaması uygulanmalıdır.

## 22.4 Arbitrary command

Provider metadata'sından shell command oluşturulmamalıdır.

`kiwix-serve` mümkün olduğunca shell aradan çıkarılmadan process API ile başlatılmalıdır.

## 22.5 Browser exposure

Internal filesystem path'leri gereksiz şekilde frontend'e sızdırılmamalıdır.

---

# 23. Packaging

## 23.1 Windows

Ana kullanıcı artefact'ı:

```text
Engelsiz-Setup.exe
```

Installer:

- Engelsiz executable'ını,
- gerekli Kiwix runtime'ını,
- gerekli lisans/notice dosyalarını

kurmalıdır.

Kullanıcı ayrıca Kiwix indirmemelidir.

Installer teknolojisi implementasyon aşamasında seçilebilir; ürün gereksinimi tek, kolay Windows setup deneyimidir.

## 23.2 Linux

Ana dağıtım artefact'ı tek indirilebilir paket olmalıdır.

Tercih:

```text
engelsiz-linux-x64.tar.gz
```

İçinde örneğin:

```text
engelsiz
runtime/kiwix-serve
LICENSES/
README
```

bulunabilir.

Buradaki "tek paket" gereksinimi kullanıcının ayrı bağımlılık kurmaması anlamına gelir; v0.1 için bütün runtime'ı tek ELF binary içine gömmek zorunlu değildir.

İleride AppImage değerlendirilebilir ancak v0.1 zorunluluğu değildir.

---

# 24. Build ve Release

CI/CD için GitHub Actions kullanılabilir.

Minimum release matrix:

```text
windows-amd64
linux-amd64
```

ARM ve macOS v0.1 kapsamında değildir.

Pipeline:

1. frontend dependencies
2. frontend production build
3. Go build içine frontend embed
4. test
5. platform runtime dosyalarını ekleme
6. package
7. checksum üretme
8. GitHub Release artefact'ı oluşturma

---

# 25. Lisans ve Third-party Notices

`kiwix-serve` ve ilgili Kiwix bileşenlerinin lisans koşulları release öncesinde doğrulanmalı ve yerine getirilmelidir.

Özellikle:

- kullanılan binary'nin lisansı,
- kaynak kod erişim yükümlülükleri,
- lisans metni dağıtımı,
- third-party notice gereklilikleri

release pipeline'ın parçası olmalıdır.

Bu konu "sonra bakarız" şeklinde ertelenmemelidir.

Aynı zamanda Engelsiz'in kendi lisansı Kiwix dağıtım modeli ile uyumlu seçilmelidir.

---

# 26. Önerilen Repo Yapısı

```text
engelsiz/
├── cmd/
│   └── engelsiz/
│       └── main.go
│
├── internal/
│   ├── app/
│   ├── api/
│   ├── config/
│   ├── database/
│   ├── download/
│   ├── packages/
│   ├── providers/
│   │   └── kiwix/
│   ├── runtime/
│   │   └── kiwix/
│   ├── search/
│   └── web/
│
├── frontend/
│   ├── src/
│   ├── public/
│   ├── package.json
│   └── vite.config.ts
│
├── migrations/
│
├── scripts/
│
├── packaging/
│   ├── windows/
│   └── linux/
│
├── third_party/
│   └── notices/
│
├── tests/
│
├── go.mod
├── go.sum
├── README.md
└── main-spec.md
```

Kesin klasör isimleri implementasyon sırasında küçük ölçüde değişebilir.

Ana sınırlar korunmalıdır:

- UI ayrı,
- Go core ayrı,
- provider mantığı ayrı,
- download manager ayrı,
- Kiwix runtime lifecycle ayrı.

---

# 27. Korunması Gereken Mimari Sınırlar

Aşağıdaki coupling'lerden kaçınılmalıdır:

### Yanlış

```text
React → doğrudan Kiwix catalog
```

### Doğru

```text
React → Engelsiz API → KiwixProvider → Kiwix
```

---

### Yanlış

```text
Download Manager içinde wikipedia_tr özel kodları
```

### Doğru

```text
KiwixProvider download metadata üretir
                ↓
Generic Download Manager indirir
```

---

### Yanlış

```text
Core'un her yerinde kiwix-specific if blokları
```

### Doğru

Kiwix-specific mantık provider/runtime paketleri içinde tutulur.

---

### Yanlış

Gelecekte lazım olabilir diye şu anda:

```text
10 provider interface
plugin marketplace
dynamic loader
generic workflow engine
```

oluşturmak.

### Doğru

v0.1 için gerekli minimum abstraction.

---

# 28. v0.1 Kullanıcı Akışı

İlk kurulumdan itibaren beklenen deneyim:

```text
1. Kullanıcı Engelsiz'i kurar.
        ↓
2. Engelsiz'i açar.
        ↓
3. Browser otomatik açılır.
        ↓
4. "Henüz bilgi paketi kurulu değil" ekranı görülür.
        ↓
5. Türkçe Wikipedia seçilir.
        ↓
6. Mini / Nopic / Maxi seçenekleri görülür.
        ↓
7. Kullanıcı varyant seçer.
        ↓
8. Disk alanı kontrol edilir.
        ↓
9. Download başlar.
        ↓
10. Progress gösterilir.
        ↓
11. Download tamamlanır.
        ↓
12. Checksum doğrulanır.
        ↓
13. Paket installed olarak kaydedilir.
        ↓
14. Kiwix runtime içeriği görür.
        ↓
15. Kullanıcı interneti kapatır.
        ↓
16. Engelsiz açılır.
        ↓
17. Wikipedia'da arama yapar.
        ↓
18. Makaleyi okur.
```

v0.1'in ana başarı ölçütü bu akışın güvenilir biçimde çalışmasıdır.

---

# 29. Acceptance Criteria

## AC-01 — Windows kurulumu

**Given** temiz bir Windows makine\
**When** kullanıcı `Engelsiz-Setup.exe` ile uygulamayı kurar\
**Then**

- Go kurulumu gerekmemeli,
- Node.js kurulumu gerekmemeli,
- Kiwix kurulumu gerekmemeli,
- kullanıcı uygulamayı normal şekilde çalıştırabilmelidir.

## AC-02 — Linux kurulumu

**Given** desteklenen Linux x86_64 makine\
**When** kullanıcı release paketini açıp Engelsiz'i çalıştırır\
**Then**

- ayrıca Kiwix indirmemeli,
- ayrıca Node/Go kurulumu yapmamalı,
- localhost UI açılabilmelidir.

## AC-03 — Localhost güvenliği

Engelsiz ve internal Kiwix runtime varsayılan olarak yalnızca loopback üzerinde erişilebilir olmalıdır.

LAN üzerindeki başka bir cihaz varsayılan ayarlarla bağlanamamalıdır.

## AC-04 — Browser startup

Engelsiz açıldığında backend hazır olduktan sonra varsayılan browser doğru localhost URL'sine açılmalıdır.

## AC-05 — Canlı katalog

Türkçe Wikipedia seçenekleri tarihli ZIM URL'leri source code'a hard-code edilmeden Kiwix katalog metadata'sından çözülmelidir.

## AC-06 — Varyantlar

Kullanıcıya uygun oldukları durumda:

- Mini
- Nopic
- Maxi

seçenekleri gösterilmelidir.

Her seçenek için mevcut metadata'dan boyut ve sürüm bilgisi gösterilmelidir.

## AC-07 — Download

Büyük ZIM download sırasında:

- progress görünmeli,
- `.part` kullanılmalı,
- uygulama kapanırsa download state kaybolmamalı,
- yeniden açıldığında resume mümkün olmalıdır.

## AC-08 — Disk alanı

Yeterli disk alanı yoksa download başlamamalı ve kullanıcıya anlaşılır hata gösterilmelidir.

## AC-09 — Verification

Download tamamlandıktan sonra desteklenen doğrulama/checksum mekanizması başarılı olmadan paket `installed` durumuna geçmemelidir.

## AC-10 — Offline kullanım

Bir Wikipedia ZIM'i kurulduktan sonra internet tamamen kapatıldığında kullanıcı:

- Engelsiz'i açabilmeli,
- kurulu paketi görebilmeli,
- arama yapabilmeli,
- makale açabilmeli.

## AC-11 — Catalog offline failure

İnternet yokken katalog isteği başarısız olsa bile kurulu Wikipedia kullanılabilir kalmalıdır.

## AC-12 — Update kontrolü

Yeni upstream ZIM bulunduğunda kullanıcı yeni sürüm olduğunu görebilmelidir.

Büyük update kullanıcı onayı olmadan otomatik başlamamalıdır.

## AC-13 — Safe update

Yeni ZIM:

- tamamen indirilmeden,
- doğrulanmadan,
- runtime tarafından kullanılabilir hale gelmeden

eski çalışan ZIM silinmemelidir.

## AC-14 — Child process cleanup

Engelsiz kontrollü kapandığında kendi başlattığı `kiwix-serve` process'i arkada orphan process olarak kalmamalıdır.

## AC-15 — Port collision

Tercih edilen localhost portu kullanımda ise uygulama uygun başka bir loopback portu kullanarak çalışabilmelidir.

## AC-16 — Provider boundary

Frontend Kiwix katalog veya runtime endpoint'lerine doğrudan bağımlı olmamalıdır.

Kiwix erişimi Go core üzerinden yapılmalıdır.

## AC-17 — Generic download manager

Download Manager içinde yalnızca Türkçe Wikipedia'ya özel URL veya business logic bulunmamalıdır.

## AC-18 — Scope kontrolü

v0.1 implementasyonu:

- local AI,
- harita,
- Kolibri,
- EPUB,
- mobil app

gibi kapsam dışı modülleri geliştirmemelidir.

---

# 30. v0.1 Definition of Done

v0.1 tamamlanmış sayılabilmesi için:

1. Windows release kurulabilir olmalı.
2. Linux release bağımsız çalışabilmeli.
3. Engelsiz browser tabanlı localhost UI açmalı.
4. Türkçe Wikipedia katalogdan keşfedilebilmeli.
5. Mini/Nopic/Maxi seçenekleri yönetilebilmeli.
6. Bir ZIM güvenli şekilde indirilebilmeli.
7. Yarım download devam ettirilebilmeli.
8. Download doğrulanabilmeli.
9. ZIM Kiwix ile serve edilebilmeli.
10. Search çalışmalı.
11. Makale açılabilmeli.
12. İnternet kapalıyken 9–11 çalışmaya devam etmeli.
13. Güncelleme tespit edilebilmeli.
14. Güncelleme eski çalışan paketi doğrulama öncesi silmemeli.
15. Uygulama kapanınca Kiwix child process kapanmalı.
16. Acceptance criteria testlerle veya belgelenmiş manuel release testleriyle doğrulanmalıdır.

---

# 31. v0.1 Sonrası Muhtemel Yönler

Bu bölüm roadmap değildir; yalnızca mimari kararların geleceği engellememesi için referanstır.

İleride değerlendirilebilecek alanlar:

```text
Kiwix
├── Wikipedia
├── WikiHow
├── Project Gutenberg
└── diğer ZIM içerikleri

Maps
└── OpenStreetMap / PMTiles

Education
└── Kolibri

Documents
├── EPUB
└── PDF

AI
├── llama.cpp
├── local embeddings
├── retrieval
└── source-backed answers
```

Bu özelliklerin hiçbiri v0.1 geliştirme kapsamını genişletmemelidir.

---

# 32. Ana Teknik Kararlar Özeti

| Konu | Karar |
|---|---|
| Core | Go |
| UI | React + TypeScript + Vite |
| UI çalışma şekli | Localhost / browser |
| App DB | SQLite |
| İlk provider | Kiwix |
| İlk içerik | Türkçe Wikipedia |
| Offline format | ZIM |
| Search | Kiwix |
| Windows dağıtımı | `Engelsiz-Setup.exe` |
| Linux dağıtımı | Tek indirilebilir tarball |
| Kiwix kurulumu | Engelsiz ile paketlenir |
| Network | `127.0.0.1` only |
| Download | Resume + `.part` + retry + checksum |
| Update | Kullanıcı onaylı |
| v0.1 AI | Yok |
| v0.1 Maps | Yok |
| Mimari yön | Provider tabanlı, minimum abstraction |

---

# 33. Uygulama Prensibi

Engelsiz v0.1 için temel kural:

> **Önce tek bir offline bilgi akışını kusursuz çalıştır. Daha sonra kapsamı genişlet.**

İlk sürümün amacı her türlü offline bilgiyi desteklemek değildir.

İlk sürümün amacı:

> Kullanıcının kolayca Engelsiz'i kurup Türkçe Wikipedia'yı indirmesi ve internet bağlantısını tamamen kestikten sonra hâlâ güvenilir biçimde arama yapıp makale okuyabilmesidir.

Bu davranış sağlandığında Engelsiz'in temel teknik ve ürün hipotezi doğrulanmış olacaktır.
