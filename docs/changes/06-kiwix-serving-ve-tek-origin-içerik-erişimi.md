/opsx-propose Engelsiz'de kurulu ZIM dosyalarının kiwix-serve üzerinden servis edilmesi ve Engelsiz localhost origin'i altında erişilmesi change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Kurulu Türkçe Wikipedia ZIM paketinin internal kiwix-serve tarafından açılması fakat kullanıcının ayrı bir Kiwix portu görmemesi.

Kapsam:
- Kurulu ZIM dosyalarını Kiwix runtime'a tanıt.
- Kiwix runtime gerektiğinde refresh/restart edilebilsin.
- Go reverse proxy oluştur.
- Kullanıcı yalnızca Engelsiz origin'ini görsün:
  - /api/* -> Go API
  - frontend/assets -> Engelsiz UI
  - /content/* -> internal kiwix-serve
- Internal Kiwix portu frontend'e gösterilmesin.
- Proxy yalnızca local trusted runtime'a yönlensin.
- Kiwix başlatılamazsa kullanıcıya anlaşılır runtime error state göster.
- Başarılı kurulumdan sonra ZIM restart gerektirmeden veya kontrollü runtime refresh sonrasında kullanılabilir hale gelsin.

Kapsam dışı:
- özel Wikipedia renderer
- özel FTS
- update mekanizması

Kiwix'in zaten yaptığı content rendering işini yeniden implement etme.
Acceptance criteria ve reverse proxy/runtime refresh senaryolarını proposal içinde tanımla.