/opsx-propose Engelsiz v0.1 için resilience, error handling, logging ve security hardening change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Ana v0.1 akışı tamamlandıktan sonra uygulamayı beklenen hata durumlarında güvenli ve anlaşılır hale getirmek.

Kapsam:
- Kullanıcıya raw stack trace gösterme.
- Teknik detayları loglarda tut.
- File-based temel logging:
  - startup
  - shutdown
  - selected ports
  - Kiwix start/stop
  - catalog errors
  - download errors
  - checksum errors
  - DB migration errors
- Basit log rotation.
- Offline internet durumu.
- Kiwix catalog unavailable.
- Disk full.
- Download interruption.
- Checksum failure.
- kiwix-serve startup failure.
- Port collision.
- SQLite migration/database error.
- DB hatasında otomatik destructive reset yapma.
- Path traversal koruması.
- Download target canonical path validation.
- Provider metadata'sından shell command üretme.
- kiwix-serve shell yerine process API ile başlatılsın.
- Internal filesystem path'leri frontend'e gereksiz expose edilmesin.
- Loglara hassas veri veya devasa response body yazılmasın.

Mevcut çalışan davranışları değiştirecek kapsam dışı refactor yapma.

Acceptance criteria ve failure-mode test matrisi oluştur.