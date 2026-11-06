Engelsiz 1.0.0 - Linux x86_64

Desteklenen temel ortam: Linux kernel 4.4 veya üstü, x86_64 masaüstü, varsayılan
web tarayıcısı ve xdg-open. Uygulama ve Kiwix statik ELF dosyalarıdır; belirli bir
host glibc sürümü veya ayrıca Go/Node/SQLite/Kiwix/PMTiles kurulumu gerekmez.

Arşivi açın ve engelsiz dosyasını çalıştırın:
  tar -xzf engelsiz-linux-x64.tar.gz
  ./engelsiz/engelsiz

Çalışma dizininiz farklı olabilir; runtime uygulamanın yanındaki runtime/
klasöründen bulunur. Bu klasörü uygulama ile birlikte tutun.
Tarayıcı otomatik açılmazsa konsolda yazan 127.0.0.1 adresini elle açın.
Başsız kullanım: ./engelsiz/engelsiz --no-browser
Temiz kapanış: Ctrl+C.

Veriler $XDG_DATA_HOME/engelsiz (varsayılan ~/.local/share/engelsiz), ayarlar
$XDG_CONFIG_HOME/engelsiz (varsayılan ~/.config/engelsiz), önbellek
$XDG_CACHE_HOME/engelsiz (varsayılan ~/.cache/engelsiz) altındadır.
Program klasörünü silmek indirilen kullanıcı içeriğini silmez.

İlk katalog/indirme ve harita çıkarma internet gerektirir. Kurulu içerik ve haritalar çevrimdışı kullanılabilir.
LICENSES/ altında lisanslar ve runtime corresponding-source materyali vardır.

AI Asistanı isteğe bağlıdır: Asistan sekmesinde motoru ve yalnızca istediğiniz
Lite/Balanced/Quality modelini açıkça indirin. İndirmeler doğrulanır; kurulumdan
sonra sorular ve kaynaklar cihazda işlenir. AI olmadan diğer modüller çalışır.
Hazır CPU AI motoru glibc 2.35+, paketli çeviri motoru glibc 2.38+ gerektirir.
Model RAM hedefleri 4/8/12-16 GB'dır; donanım garantisi değildir. AI-README.md
kurulum ve sınırları açıklar. Kaynak metnini AI yanıtından ayrı değerlendirin.
