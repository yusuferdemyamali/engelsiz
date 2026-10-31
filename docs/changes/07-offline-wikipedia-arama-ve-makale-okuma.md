/opsx-propose Engelsiz v0.1 offline Wikipedia search ve article reading change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Kurulu Türkçe Wikipedia ZIM üzerinde internet olmadan arama yapmak ve makale okuyabilmek.

Kapsam:
- Engelsiz kendi Wikipedia full-text index'ini oluşturmasın.
- Kiwix'in mevcut search yeteneğini kullan.
- Search akışı:
  React -> Engelsiz API -> KiwixProvider -> kiwix-serve.
- GET /api/search benzeri endpoint oluştur.
- Search sonucu mümkün olduğunca:
  - title
  - snippet
  - source
  - article identifier/link
  döndürsün.
- Search UI oluştur.
- Empty state / no results state oluştur.
- Search sonucu seçildiğinde article açılabilsin.
- Article içeriği Kiwix'in mevcut rendering mekanizması üzerinden sunulsun.
- Engelsiz navigation'a geri dönüş sağlansın.
- İnternet tamamen kapalıyken:
  - uygulama açılsın
  - kurulu paket görülsün
  - search çalışsın
  - article açılsın
- Katalog internet yüzünden hata verse bile offline içerik kullanılabilir kalsın.

Kapsam dışı:
- custom Wikipedia renderer
- SQLite FTS
- embeddings
- RAG
- local AI

Acceptance criteria içinde gerçek offline test senaryosu mutlaka yer alsın.