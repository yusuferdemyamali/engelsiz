/opsx-propose Engelsiz v0.1 CI/CD ve release pipeline change'ini planla.

Kaynak olarak main-spec.md dosyasını esas al.

Amaç:
Windows ve Linux release artefact'larını tekrarlanabilir şekilde build/test/package edip GitHub Release'e hazırlamak.

Kapsam:
GitHub Actions pipeline oluştur.

Minimum matrix:
- windows-amd64
- linux-amd64

Pipeline:
1. frontend dependencies
2. frontend production build
3. Go build + embedded frontend
4. unit/integration tests
5. platforma uygun kiwix runtime'ın eklenmesi
6. Windows installer / Linux tarball packaging
7. third-party license/notice doğrulaması
8. release artefact checksum üretimi
9. GitHub Release artefact üretimi

Ayrıca v0.1 Definition of Done için release validation checklist oluştur:
- Windows temiz kurulum
- Linux bağımsız çalışma
- localhost startup
- catalog
- Mini/Nopic/Maxi
- download
- resume
- checksum
- Kiwix serving
- offline search
- article reading
- safe update
- child process cleanup
- port collision

Otomatik test edilemeyen kriterleri belgelenmiş manuel release testleri olarak tanımla.

Kapsam dışı:
- macOS
- ARM
- application auto-update
- nightly release zorunluluğu

main-spec.md acceptance criteria ve Definition of Done ile birebir izlenebilirlik sağla.