/opsx-propose Engelsiz v0.1 Kiwix Wikipedia update mekanizmasını planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Kurulu Türkçe Wikipedia sürümü ile Kiwix kataloğundaki güncel sürümü karşılaştırmak ve kullanıcı onayıyla güvenli update yapmak.

Kapsam:
- Kurulu package version ile upstream katalog metadata'sını karşılaştır.
- Yeni sürüm varsa update_available state üret.
- UI:
  - kurulu sürüm
  - yeni sürüm
  - yeni dosyanın boyutu
  - Güncelle aksiyonu
- Büyük ZIM update kullanıcı onayı olmadan otomatik başlamasın.
- Existing generic Download Manager kullanılsın.
- Update:
  old.zim çalışmaya devam ederken new.zim.part indirilsin.
- Yeni dosya:
  - tamamen indirilmeden
  - checksum doğrulanmadan
  - runtime tarafından kullanılabilir hale gelmeden
  eski ZIM silinmesin.
- Yeni runtime başarıyla kullanılabilir olduktan sonra eski sürüm temizlenebilsin.
- Update başarısız olursa eski çalışan Wikipedia bozulmadan devam etsin.

Kapsam dışı:
- incremental ZIM patch
- background automatic update
- torrent
- mirror switching
- Engelsiz uygulamasının kendi binary auto-update sistemi

Acceptance criteria içinde failed update ve rollback/old-version preservation senaryoları bulunmalı.