/opsx-propose Engelsiz'de indirilen Kiwix ZIM paketlerinin doğrulanması, kurulması ve registry'de yönetilmesi change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Download tamamlandıktan sonra ZIM'i güvenli biçimde doğrulamak, atomik olarak kurmak ve kurulu içerik state'ini SQLite'ta tutmak.

Kapsam:
- content_packages SQLite tablosu/migration'ı.
- Paket metadata:
  - provider
  - provider_package_id
  - content_type
  - language
  - variant
  - title
  - version
  - source_url
  - local_path
  - size
  - checksum
  - status
  - timestamps
- Download tamamlandıktan sonra desteklenen checksum doğrulaması.
- Verification başarılı olmadan paket installed durumuna geçmesin.
- Checksum hatasında bozuk dosya kurulu paket olarak görünmesin.
- .part -> final .zim geçişi atomic finalize ile yapılsın.
- ZIM dosyaları OS user-data altındaki content/kiwix dizininde saklansın.
- Kurulu paketler API üzerinden listelenebilsin.
- UI kurulu varyantı ve sürümü gösterebilsin.
- Kurulum yarıda kalırsa tutarlı state korunsun.
- Internal filesystem path'leri frontend'e gereksiz expose edilmesin.

Kapsam dışı:
- update version replacement
- Kiwix search
- custom ZIM parser

Wikipedia içeriğini SQLite'a import etme.
Acceptance criteria ve checksum/install failure senaryolarını proposal içinde tanımla.