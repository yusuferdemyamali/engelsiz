/opsx-propose Engelsiz için provider'dan bağımsız generic Download Manager change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Kiwix ZIM dosyaları gibi büyük dosyaları güvenli ve devam ettirilebilir şekilde indirebilen generic bir download altyapısı oluşturmak.

Kapsam:
- HTTP/HTTPS streaming download.
- .part dosyası.
- HTTP Range ile resume.
- Uygulama kapanıp açıldığında download state korunmalı.
- Progress:
  - downloaded bytes
  - total bytes
  - percentage
- Pause/resume.
- Cancel.
- Retry.
- Disk alanı pre-check.
- Yetersiz alanda download başlamasın.
- downloads SQLite tablosu/migration'ı.
- Download state persistence.
- Download API endpoint'leri.
- Download ekranı:
  - dosya/paket
  - varyant
  - progress
  - downloaded/total
  - status
  - pause/resume
  - cancel
  - hata mesajı
- Server Range desteklemiyorsa güvenli fallback davranışı belirle.

Download Manager içinde:
- wikipedia_tr
- Kiwix'e özel URL üretimi
- Mini/Nopic/Maxi business logic

bulunmasın.

Provider download metadata üretir, generic manager yalnızca transferi gerçekleştirir.

Kapsam dışı:
- checksum doğrulama/final install
- kiwix-serve refresh
- update sistemi

Acceptance criteria, restart/resume ve disk-full testlerini proposal içinde tanımla.