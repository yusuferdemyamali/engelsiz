/opsx-propose Engelsiz v0.1 Windows dağıtım ve installer change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Temiz Windows x86_64 makinede kullanıcının hiçbir developer/runtime bağımlılığı kurmadan Engelsiz'i tek setup üzerinden kurabilmesi.

Kapsam:
- Ana kullanıcı artefact'ı Engelsiz-Setup.exe olsun.
- Installer teknolojisini mevcut mimariye en uygun şekilde seç.
- Installer:
  - Engelsiz executable
  - gerekli frontend assets (binary içine embed ise ayrıca gerekmez)
  - Windows kiwix-serve runtime
  - gerekli runtime dosyaları
  - LICENSES / third-party notices
  kurmalı.
- Kullanıcıdan ayrıca:
  - Go
  - Node/npm
  - SQLite
  - Kiwix
  - kiwix-serve
  - Docker
  kurulması istenmemeli.
- Uygulama Windows user app-data dizinlerini kullanmalı.
- Install/uninstall davranışı güvenli olmalı.
- Kullanıcının indirdiği büyük ZIM content'i uninstall sırasında otomatik silinecekse bunun davranışı açıkça değerlendirilsin; veri kaybına yol açacak sürpriz davranış oluşturma.
- Temiz Windows VM/manual release test akışı tanımla.
- Kiwix binary dağıtımındaki lisans yükümlülüklerini araştırıp proposal içinde açıkça tanımla.

Kapsam dışı:
- macOS
- Microsoft Store
- auto-update
- ARM

Acceptance criteria AC-01'i eksiksiz karşıla.