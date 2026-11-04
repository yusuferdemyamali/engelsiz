Engelsiz - Windows 10/11 x86_64

Engelsiz-Setup.exe dosyasını çalıştırın. Yönetici izni gerekmez. Başlat menüsündeki
Engelsiz kısayolunu açın; varsayılan tarayıcı yerel arayüze bağlanır.
Go, Node, SQLite, Kiwix veya PMTiles kurmanız gerekmez.

Çeviri motoru için önkoşul: Microsoft VC++ 2015-2022 Yeniden Dağıtılabilir
(x64) paketi kurulu olmalıdır (çoğu Windows 10/11 sisteminde hazır gelir).
Kurulu değilse çeviri çalışmaz; Wikipedia, haritalar ve indirmeler etkilenmez.
Kurulum: https://learn.microsoft.com/cpp/windows/latest-supported-vc-redist
adresinden "X64" bağlantısı. Motor Python/pip gerektirmez; runtime/translation
altında uygulamayla gelir.

Uygulama yalnızca 127.0.0.1 üzerinde çalışır. İlk içerik indirmesi internet gerektirir.
İçerik ve harita kurulduktan sonra çevrimdışı kullanılabilir.
Konsol penceresini Ctrl+C ile kapatmak uygulamayı temiz biçimde durdurur.

Program dosyaları: %LOCALAPPDATA%\Programs\Engelsiz
İçerik/veritabanı: %LOCALAPPDATA%\Engelsiz
Ayarlar/loglar: %APPDATA%\Engelsiz

Kaldırma yalnızca kurulu programı/kısayolları kaldırır. İndirdiğiniz ZIM dosyaları,
yarım indirmeler, veritabanı ve ayarlar korunur. Bunları silmek istiyorsanız önce
Engelsiz'i kapatın ve bu kullanıcı veri dizinlerini kendiniz kaldırın.

LICENSES klasöründe bileşen lisansları ve runtime kaynak materyali bulunur.

Çeviri (Türkçe-İngilizce, İngilizce-Türkçe): modeller toplam ~245 MB'tır ve
gizlilik için uygulamaya gömülü gelmez; Çeviri sekmesindeki "İndir" düğmesiyle
açıkça indirilir. Kurulumdan sonra çeviri tamamen çevrimdışı çalışır.
Kaldırıp yeniden kursanız bile indirdiğiniz modeller ve veritabanı korunur.
