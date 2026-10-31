/opsx-propose Engelsiz v0.1 Kiwix katalog entegrasyonunu planla.

Kaynak olarak [[engelsiz-main_spec]] dosyasını esas al.

Amaç:
Kiwix'in canlı kataloğundan Türkçe Wikipedia paketlerini dinamik olarak keşfetmek ve kullanıcıya Mini / Nopic / Maxi seçeneklerini göstermek.

Kapsam:
- KiwixProvider canlı Kiwix katalog metadata'sını okusun.
- Tarihli ZIM URL'leri source code içine hard-code edilmesin.
- Yalnızca Türkçe + Wikipedia sonuçları v0.1 UI'ına sunulsun.
- Mini, Nopic ve Maxi varyantları normalize edilmiş ortak model olarak döndürülsün.
- Mümkün olan metadata:
  - provider package id
  - başlık
  - varyant
  - sürüm/yayın tarihi
  - boyut
  - açıklama
  - download metadata
- Boyut veya tarih sabit yazılmasın.
- Frontend doğrudan Kiwix'e bağlanmasın.
- Akış React -> Engelsiz API -> KiwixProvider -> Kiwix şeklinde olsun.
- /api/catalog benzeri API oluştur.
- Bilgi Paketleri ekranında üç varyant gösterilsin.
- Katalog erişilemiyorsa anlaşılır offline/error state göster.
- Katalog hatası uygulamanın geri kalanını çökertmesin.

Kapsam dışı:
- download
- ZIM kurulumu
- search
- article read
- update
- diğer Kiwix içerikleri

v0.1 yalnızca Türkçe Wikipedia'dır.
Acceptance criteria ve test senaryolarını proposal içinde açıkça tanımla.