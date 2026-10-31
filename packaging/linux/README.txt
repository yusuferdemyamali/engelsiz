Engelsiz - Linux x86_64

Desteklenen temel ortam: Linux kernel 4.4 veya üstü, x86_64 masaüstü, varsayılan
web tarayıcısı ve xdg-open. Uygulama ve Kiwix statik ELF dosyalarıdır; belirli bir
host glibc sürümü veya ayrıca Go/Node/SQLite/Kiwix kurulumu gerekmez.

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

İlk katalog/indirme internet gerektirir. Kurulu içerikte arama/okuma çevrimdışıdır.
LICENSES/ altında lisanslar ve runtime corresponding-source materyali vardır.
