/opsx-propose Engelsiz'e minimum ContentProvider mimarisini ve Kiwix runtime lifecycle yönetimini ekleyecek change'i planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Core'u Kiwix'e tamamen hard-code etmeden, v0.1 için yalnızca KiwixProvider bulunan minimum provider sınırını oluşturmak ve kiwix-serve process'ini Engelsiz tarafından yönetmek.

Kapsam:
- Minimum ContentProvider interface tasarla.
- Yalnızca gerçek KiwixProvider implementasyonu oluştur.
- Dynamic plugin loading, plugin SDK veya marketplace yapma.
- kiwix-serve executable path çözümleme mekanizması oluştur.
- kiwix-serve Engelsiz tarafından child process olarak başlatılsın.
- Internal Kiwix yalnızca 127.0.0.1 üzerinde dinlesin.
- Internal port kullanıcıya expose edilmesin.
- PID/process state takip edilsin.
- Aynı Engelsiz instance'ı aynı anda birden fazla gereksiz kiwix-serve process'i başlatmasın.
- Beklenmedik process kapanışı algılanabilsin.
- Gerektiğinde kontrollü restart altyapısı olsun.
- Engelsiz kapanınca kendi başlattığı kiwix-serve process'i kapansın.
- stdout/stderr ileride loglanabilecek şekilde yakalansın.

Development ortamında kiwix-serve binary konfigüre edilebilir olabilir.
Windows/Linux release packaging bu change kapsamında değildir.

Kapsam dışı:
- Kiwix katalog sorgusu
- ZIM indirme
- Wikipedia search
- article UI
- release packaging

Minimum abstraction kullan.
Acceptance criteria ve lifecycle testlerini proposal içinde tanımla.