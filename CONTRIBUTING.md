# Katkıda bulunmak / Contributing

Türkçe veya İngilizce hata raporları, kurulum testleri, dokümantasyon, çeviri ve kod katkıları kabul edilir. Turkish and English contributions are welcome.

## Hata bildirimi ve öneriler / Reports and suggestions

[Issue şablonlarından](https://github.com/yusuferdemyamali/engelsiz/issues/new/choose) uygun olanı seçin. Önce mevcut açık ve kapalı issue’larda aynı konunun olup olmadığını kontrol edin. Hata bildirmek için OpenSpec belgesi hazırlamanız gerekmez.

Include the application version, OS/architecture, reproduction steps, expected behavior, and actual behavior. For offline/download problems, say which archive and which stage failed. Remove personal paths, tokens, and unrelated private content from logs or screenshots before posting.

## Yerel geliştirme / Local development

Geliştirme araçları: Go **1.26.0**, Node **22** ve npm. Paketleme script’leri için Python **3.12+**, Windows installer derlemesi için Inno Setup **6** kullanılır. Bunlar son kullanıcı bağımlılıkları değildir.

Run the following in Bash (Git Bash on Windows):

```sh
git clone https://github.com/yusuferdemyamali/engelsiz.git
cd engelsiz
npm ci --prefix frontend
bash scripts/build-frontend.sh
go run ./cmd/engelsiz --no-browser
```

Open the loopback address printed by the application. To serve installed ZIM content, provide a compatible local Kiwix executable using `--kiwix-binary /absolute/path/to/kiwix-serve` (`kiwix-serve.exe` on Windows). The development override and runtime lifecycle are separate from building a redistributable package; see [release validation](docs/release-validation.md).

## Kontroller / Checks

Choose checks appropriate to the change. The platform CI also runs the full frontend build and platform-specific checks.

```sh
node --test frontend/tests/*.test.mjs
go test ./...
go vet ./...
go test -race ./internal/app ./internal/packages ./internal/download ./internal/runtime/kiwix
python -m unittest discover -s scripts/tests -v
python scripts/collect-app-notices.py --check
```

The notice check uses the exact Go toolchain above and the installed, locked npm dependencies. If dependency inputs change, regenerate with `python scripts/collect-app-notices.py` and include the reviewed attribution changes. Real package builds additionally require matching runtime sources, notices, and compliance inputs.

## Pull request

Create a branch from current `main`, link the relevant issue, describe the resulting behavior, and report which checks you ran. Keep changes focused; preserve unrelated local work. For feature changes, check the existing plans in `openspec/changes/` and the [product spec](docs/main-spec.md), and update the applicable plan/verification evidence when needed.

For installation acceptance, record the OS version, architecture, source commit, package checksum, and exact results. Keep unexecuted clean-machine checks pending; a successful compile does not establish that a distributable works on a clean machine. See the [acceptance procedures](docs/release-validation.md).

Contributions to Engelsiz’s own code use the project’s [MIT license](LICENSE). Keep upstream license/notice material intact when changing bundled third-party inputs.
