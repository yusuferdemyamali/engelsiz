/opsx-propose application-foundation

Engelsiz v0.1 için temel uygulama iskeletini oluşturacak change'i planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Go core + React/TypeScript/Vite frontend + SQLite kullanan localhost tabanlı Engelsiz uygulamasının temelini oluşturmak.

Kapsam:
- Önerilen repo yapısını oluştur.
- Go uygulama entrypoint ve temel app lifecycle yapısını kur.
- React + TypeScript + Vite frontend iskeletini oluştur.
- Tailwind CSS ve shadcn/ui temel entegrasyonunu hazırla.
- Frontend production build'inin Go binary içine embed edilmesini planla.
- Go HTTP server yalnızca 127.0.0.1 üzerinde çalışsın.
- Tercih edilen port 17321 olsun; doluysa otomatik uygun loopback portu seçilsin.
- GET /api/health endpoint'i olsun.
- Backend hazır olduktan sonra varsayılan browser gerçek kullanılan localhost adresiyle açılsın.
- OS standartlarına uygun config/data/cache dizinlerini oluştur:
  - Windows user app-data
  - Linux XDG
- SQLite bağlantısı ve migration altyapısını kur.
- Temel settings/provider metadata altyapısı hazırlanabilir fakat henüz Kiwix business logic ekleme.
- Temiz startup/shutdown lifecycle oluştur.

Kapsam dışı:
- Kiwix catalog
- kiwix-serve
- ZIM
- download manager
- Wikipedia UI
- search
- update sistemi
- packaging

Gereksiz abstraction veya gelecekte kullanılabilir diye provider/plugin sistemi geliştirme.

Acceptance criteria ve test yaklaşımını proposal içinde açıkça tanımla.
Bu change yalnızca foundation/app shell kapsamını içersin.