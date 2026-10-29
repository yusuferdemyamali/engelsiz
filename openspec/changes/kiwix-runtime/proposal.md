# Change: kiwix-runtime

## Why

Engelsiz'in çalışan localhost foundation'ına, core'u Kiwix komutlarına bağımlı kılmadan yönetilen bir içerik runtime'ı eklemek gerekir. Change 02, sonraki içerik akışları için gerçek child-process lifecycle'ını kurar; boş ilk açılış ayrıca Kiwix kurulmasını veya içerik bulunmasını gerektirmez.

## What Changes

- Yalnızca bu change'in kullandığı lifecycle işlemlerini taşıyan küçük Go `ContentProvider` sınırı ve tek gerçek `KiwixProvider` oluştur.
- Development executable override'ı ve executable'a göre çözümlenen `runtime/kiwix-serve[.exe]` adayı kullan; yalnızca başlatma gerektiğinde çözümle.
- Kiwix'i shell/daemon olmadan, yalnızca `127.0.0.1` ve özel seçilmiş port ile başlat; readiness için bounded HTTP 200 yanında dinleyicinin başlatılan child PID'sine ait olduğunu doğrula; PID/state, tek-instance eşzamanlılık, beklenmedik exit ve açık restart yönetimini sağla.
- stdout/stderr'i sınırlı kaynak kullanımıyla ayrı yakala; start/readiness/exit/stop hatalarını mevcut log altyapısına bağla.
- Provider'ı app lifecycle'ına bağla; boş içerikte no-op başlangıç, kontrollü kapanışta yalnızca sahip olunan child'ın sonlandırılması ve reap edilmesi sağlansın.

## Capabilities

### New Capabilities

- `kiwix-runtime`: Minimum provider sınırı, executable resolution, özel loopback child process, readiness/state/restart ve app-owned cleanup.

### Modified Capabilities

Yok. Foundation'ın HTTP health/browser sözleşmesi korunur; yeni runtime davranışı ayrı capability'dir. Henüz ana `openspec/specs/` capability'si yoktur.

## Impact

`internal/providers`, concrete Kiwix provider/runtime paketleri, `internal/app`, dar development config/entrypoint girdisi ve lifecycle testleri etkilenir. Mevcut OS-standard user dizinleri ve Go 1.26 minimumu korunur. Public runtime kontrol endpoint'i, internal portu gösteren API/UI veya yeni zorunlu bağımlılık eklenmez.

## Non-Goals

Katalog, download, ZIM format parsing/doğrulama/kurulum/registry, kurulu paket keşfi, search, reverse proxy, article UI ve release packaging yoktur. Change 06 kurulu içeriği runtime'a bağlayıp tek-origin serving'i etkinleştirir. Dinamik plugin, SDK, marketplace, gelecekteki provider stub'ları, otomatik restart döngüsü, process-name/PID dosyasıyla process sahiplenme ve tam crash-proof supervisor yoktur.

## Acceptance Criteria

1. Core yalnızca kullanılan lifecycle interface'ine dayanır; tek concrete provider Kiwix'tir ve kullanılmayan katalog/search/download metotları bulunmaz.
2. İçeriksiz fresh profil, Kiwix binary'si yokken de aynı localhost shell/health/browser akışıyla açılır; child process veya binary çözümleme hatası üretmez.
3. Explicit içerik girdisiyle başlatma doğru executable'ı shell olmadan, `--address=127.0.0.1`, özel port ve ayrı path argümanlarıyla çalıştırır. Internal URL/port/path browser'a veya public API'ye verilmez.
4. Başarı ancak child canlıyken bounded HTTP 200 ve seçilen IPv4 loopback dinleyicisinin tam olarak o child PID'sine ait olduğuna dair OS kanıtı birlikte sağlanınca bildirilir; sabit bekleme süresi sahiplik kanıtı değildir. Sahiplik doğrulanamazsa fail closed uygulanır. Gecikmeli bind sırasında başka bir HTTP 200 sunucusu portu alsa bile sahiplenilmez veya öldürülmez; owned attempt reap edilir, contextual hata ya da bounded fresh-port retry oluşur. Missing executable, spawn hatası, early exit, readiness timeout/cancellation gerçek hata ve temiz kaynak bırakma ile sonuçlanır.
5. PID yalnızca canlı owned child için tutulur; eşzamanlı/tekrarlı ensure-start en fazla bir child oluşturur. Beklenmedik sıfır veya sıfır olmayan exit failed durumuna geçer ve reap edilir.
6. Açık restart eski child tamamen durup reap edilmeden yenisini başlatmaz; kendiliğinden restart fırtınası oluşmaz. Stop, restart'ın replacement readiness bekleyişi sırasında stop intent ve startup iptalini gecikmeden iletir; kazanan Stop'tan sonra önceki/queued restart başarı veya geç child üretemez. Kısa veya zaten iptal edilmiş Stop context'i owned cleanup'ı atlamaz; start/stop/restart yarışları duplicate veya orphan üretmez.
7. SIGINT/SIGTERM/context iptali ve kısmi startup hatası owned child'ı bounded stop/kill/wait ile temizler; bağımsız process'lere dokunmaz. Log kaynağı child output drain/reap tamamlanana kadar açık kalır.
8. stdout/stderr ayrı ve sınırlı biçimde yakalanır; uzun/yoğun çıktı deadlock veya sınırsız bellek oluşturmaz; teknik lifecycle hataları loglanır.

## Test Strategy

- Gerçek subprocess fixture: bind argümanları, delayed/failed HTTP readiness, early exit, beklenmedik exit, eşzamanlı ensure, açık restart sırası, stop sırasında start, startup cancellation, zorunlu kill, ayrı stdout/stderr ve yoğun çıktı. Bind'i 500 ms geciken child ile erken HTTP 200 sunan bağımsız port rakibi; geciken ve hiç hazır olmayan restart sırasında Stop, kısa/zaten iptal edilmiş Stop context'leri, reap/late-generation ve sentinel survival regresyonlarını ekle. Fixture işletim sistemi process'i olmalı; yalnızca struct/mock state testleri yeterli değildir.
- Dar private Linux procfs ve Windows PID-owner IPv4 listener yardımcıları için gerçek owned/unrelated listener ve kanıt okunamaması/uyuşmazlığı kapsamını doğrula. Linux procfs erişimi varsayımını ve native Windows'ta gerçekten çalıştırılan kontrolleri raporla; cross-build çalışma kanıtı değildir.
- App integration: boş profil/binary yok, injected gerçek fixture provider ile cancellation/startup error cleanup, HTTP health/browser foundation regresyonları, unrelated sentinel process'in korunması.
- `go test ./...`, lifecycle paketlerinde uygun `go test -race`, `go vet ./...`, format kontrolü ve `CGO_ENABLED=0` Linux/Windows amd64 build. Windows cross-build runtime execution sayılmaz.
- Gerçek `kiwix-serve` ve küçük geçerli ZIM zaten kolayca sağlanabiliyorsa isteğe bağlı gerçek smoke; binary sürümü, komut, readiness ve cleanup kaydedilir. Bulunmuyorsa fixture kanıtı ayrı raporlanır, gerçek Kiwix/Windows üzerinde çalıştırılmayan senaryolar başarı olarak yazılmaz. Packaging yapılmaz.

## References

- `docs/changes/02-content-provider-sınırı-ve-kiwix-runtime-lifecycle.md`.
- `docs/main-spec.md` §§5–8, 17, 20–22, 26–27; AC-03/14/16/18'in runtime alt kümesi.
- Resmi Kiwix [CLI ve HTTP dokümanı](https://kiwix-tools.readthedocs.io/en/latest/kiwix-serve.html), [upstream CLI source](https://github.com/kiwix/kiwix-tools/blob/4717ee15faa77c9e9641557b85af19f1fad5ea27/src/server/kiwix-serve.cpp), 2026-10-28 tarihinde incelendi. Önceki Linux gerçek-runtime smoke, yeni listener ownership ve restart cancellation regresyonlarının yerine geçmez; native Windows execution sınırı ayrıca raporlanır.
