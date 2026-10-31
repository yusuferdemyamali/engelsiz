/opsx-propose Engelsiz v0.1 Linux x86_64 release packaging change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Linux kullanıcısının ayrıca Go, Node veya Kiwix kurmadan tek indirilebilir paket ile Engelsiz'i çalıştırabilmesi.

Kapsam:
- Release artefact:
  engelsiz-linux-x64.tar.gz
- Paket içinde:
  - engelsiz binary
  - runtime/kiwix-serve
  - gerekli runtime dependency/files
  - LICENSES
  - README
- Linux XDG dizinleri doğru kullanılmalı.
- Kullanıcı ayrıca Go/Node/Kiwix kurmamalı.
- Runtime path executable konumundan güvenli şekilde çözümlenmeli.
- Paket açıldıktan sonra mümkün olduğunca basit çalıştırma deneyimi sağla.
- AppImage v0.1 zorunluluğu değildir; scope'a ekleme.
- Desteklenen Linux/glibc/runtime varsayımlarını proposal'da netleştir.
- Kiwix runtime lisans gereksinimlerini karşıla.
- Temiz desteklenen Linux ortamında release smoke test tanımla.

Kapsam dışı:
- AppImage zorunluluğu
- distro-specific deb/rpm
- ARM
- macOS

Acceptance criteria AC-02'yi eksiksiz karşıla.