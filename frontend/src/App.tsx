import { useCallback, useEffect, useRef, useState } from "react"
import { buttonClass, downloadRequest, DownloadsView, type Download } from "./downloads"
import { PackagesView } from "./packages"
import { SearchView } from "./search"

const variants = [
  { id: "mini", label: "Mini" },
  { id: "nopic", label: "Nopic" },
  { id: "maxi", label: "Maxi" },
] as const

type CatalogPackage = {
  provider: "kiwix"
  providerPackageId: string
  title: string
  language: "tr"
  variant: (typeof variants)[number]["id"]
  description?: string | null
  publishedAt?: string | null
  updatedAt?: string | null
  sizeBytes?: number | null
  download: { kind: "metalink" | "zim"; url: string; mediaType: string }
}

type CatalogState =
  | { status: "loading" }
  | { status: "ready"; packages: CatalogPackage[] }
  | { status: "error"; offline: boolean }

const dateFormatter = new Intl.DateTimeFormat("tr-TR", {
  day: "numeric",
  month: "long",
  year: "numeric",
  timeZone: "UTC",
})
const sizeFormatter = new Intl.NumberFormat("tr-TR", { maximumFractionDigits: 1 })

function formatDate(value?: string | null) {
  if (!value) return "Bilinmiyor"
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? "Bilinmiyor" : dateFormatter.format(date)
}

function formatSize(bytes?: number | null) {
  if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes <= 0) return "Bilinmiyor"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1000)), units.length - 1)
  return `${sizeFormatter.format(bytes / 1000 ** unit)} ${units[unit]}`
}

function PackageMetadata({ item }: { item: CatalogPackage }) {
  return (
    <>
      <div className="min-w-0 space-y-2">
        <p className="break-words font-medium text-parchment">{item.title}</p>
        {item.description && (
          <p className="max-w-prose break-words text-sm leading-6 text-sand/90">{item.description}</p>
        )}
      </div>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-4 text-sm md:min-w-64">
        <div className="rounded border border-sand/15 bg-coal/60 px-3 py-2">
          <dt className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Katalog boyutu</dt>
          <dd className="mt-1 font-mono tabular-nums text-parchment">{formatSize(item.sizeBytes)}</dd>
        </div>
        <div className="rounded border border-sand/15 bg-coal/60 px-3 py-2">
          <dt className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Yayın tarihi</dt>
          <dd className="mt-1 font-mono tabular-nums text-parchment">{formatDate(item.publishedAt)}</dd>
        </div>
        {!item.publishedAt && item.updatedAt && (
          <div className="col-span-2 rounded border border-sand/15 bg-coal/60 px-3 py-2">
            <dt className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Katalog güncellemesi</dt>
            <dd className="mt-1 font-mono tabular-nums text-parchment">{formatDate(item.updatedAt)}</dd>
          </div>
        )}
      </dl>
    </>
  )
}

const navEntries = [
  { id: "catalog", label: "Bilgi Paketleri", code: "01" },
  { id: "downloads", label: "İndirmeler", code: "02" },
  { id: "packages", label: "Yerel Paketler", code: "03" },
  { id: "search", label: "Ara", code: "04" },
] as const

type ViewId = (typeof navEntries)[number]["id"]

export default function App() {
  const [view, setView] = useState<ViewId>("catalog")
  const [catalog, setCatalog] = useState<CatalogState>({ status: "loading" })
  const activeRequest = useRef<AbortController | null>(null)
  const creationRequest = useRef<AbortController | null>(null)
  const [creating, setCreating] = useState<string | null>(null)
  const [creationError, setCreationError] = useState<{ id: string; message: string } | null>(null)
  const [notice, setNotice] = useState("")
  const [downloadRefresh, setDownloadRefresh] = useState(0)

  useEffect(() => {
    if (notice) document.getElementById("downloads-heading")?.focus()
  }, [notice])

  async function startDownload(item: CatalogPackage) {
    if (creationRequest.current) return
    const controller = new AbortController()
    creationRequest.current = controller
    setCreating(item.providerPackageId)
    setCreationError(null)
    setNotice("")
    try {
      await downloadRequest<{ download: Download }>("/api/downloads", controller.signal, {
        provider: item.provider,
        providerPackageId: item.providerPackageId,
      })
      if (!controller.signal.aborted) {
        setDownloadRefresh((value) => value + 1)
        setNotice("İndirme başlatıldı. İlerlemeyi İndirmeler bölümünden takip edebilirsiniz.")
        setView("downloads")
      }
    } catch (failure) {
      if (!controller.signal.aborted) {
        setCreationError({ id: item.providerPackageId, message: failure instanceof Error ? failure.message : "İndirme başlatılamadı. Tekrar deneyin." })
      }
    } finally {
      if (!controller.signal.aborted) setCreating(null)
      if (creationRequest.current === controller) creationRequest.current = null
    }
  }

  const loadCatalog = useCallback(async () => {
    if (activeRequest.current) return
    const controller = new AbortController()
    activeRequest.current = controller
    setCatalog({ status: "loading" })

    try {
      const response = await fetch("/api/catalog", { signal: controller.signal })
      if (!response.ok) throw new Error("catalog_unavailable")
      const result: { packages: CatalogPackage[] } = await response.json()
      if (!Array.isArray(result.packages)) throw new Error("invalid_catalog")
      if (!controller.signal.aborted && activeRequest.current === controller) {
        setCatalog({ status: "ready", packages: result.packages })
      }
    } catch {
      if (!controller.signal.aborted && activeRequest.current === controller) {
        setCatalog({ status: "error", offline: navigator.onLine === false })
      }
    } finally {
      if (activeRequest.current === controller) activeRequest.current = null
    }
  }, [])

  useEffect(() => {
    void loadCatalog()
    return () => {
      activeRequest.current?.abort()
      activeRequest.current = null
      creationRequest.current?.abort()
      creationRequest.current = null
    }
  }, [loadCatalog])

  return (
    <div className="min-h-screen">
      <div className="mx-auto w-full max-w-5xl px-4 py-6 sm:px-8 sm:py-8">
        <header className="terminal-panel px-5 py-5 sm:px-7">
          <div className="flex flex-wrap items-center gap-4 sm:gap-6">
            <img src="/logo.svg" alt="Engelsiz amblemi" width={64} height={64} className="h-14 w-14 shrink-0 sm:h-16 sm:w-16" />
            <div className="min-w-0 flex-1">
              <p className="terminal-label">Arşiv terminali</p>
              <h1 className="font-display text-5xl leading-none tracking-wide text-parchment sm:text-6xl">ENGELSİZ</h1>
              <p className="mt-1 text-sm text-sand/90">Bilginin erişim engeli olmaz.</p>
            </div>
            <div className="hidden text-right sm:block" aria-hidden="true">
              <p className="font-mono text-[11px] uppercase tracking-[0.18em] text-sand/70">Yerel öncelikli</p>
              <p className="mt-1 font-mono text-xs text-olive">Çevrimdışı çalışır</p>
            </div>
          </div>
        </header>

        <nav aria-label="Ana menü" className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
          {navEntries.map((entry) => {
            const active = view === entry.id
            return (
              <button
                key={entry.id}
                type="button"
                aria-current={active ? "page" : undefined}
                onClick={() => { setView(entry.id); setNotice("") }}
                className={`flex min-h-11 items-center gap-3 rounded-md border px-3 py-2 text-left text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal ${active ? "border-transparent bg-rust text-parchment" : "border-sand/20 bg-ash/80 text-sand hover:border-sand/40 hover:text-parchment"}`}
              >
                <span className={`font-mono text-[11px] tabular-nums ${active ? "text-parchment/80" : "text-sand/60"}`}>{entry.code}</span>
                {entry.label}
              </button>
            )
          })}
        </nav>

        <p role="status" className={notice ? "terminal-panel mt-4 border-olive/40 px-4 py-3 text-sm leading-6 text-parchment" : "sr-only"}>{notice}</p>

        <main className="terminal-panel mt-4 px-5 py-6 sm:px-7 sm:py-8">
          {view === "search" ? <SearchView onOpenPackages={() => setView("packages")} /> : view === "downloads" ? <DownloadsView refreshKey={downloadRefresh} /> : view === "packages" ? <PackagesView onOpenDownloads={() => setView("downloads")} /> : (
          <section aria-labelledby="catalog-heading">
            <p className="terminal-label">01 / Katalog</p>
            <h2 id="catalog-heading" className="mt-2 font-display text-4xl tracking-wide text-parchment">BİLGİ PAKETLERİ</h2>
            <p className="mt-1 text-lg text-parchment">Türkçe Wikipedia</p>
            <p className="mt-2 max-w-prose text-sm leading-6 text-sand/90">
              Kiwix kataloğundaki sürümlerin açıklamalarını, boyutlarını ve yayın tarihlerini inceleyin.
            </p>

            {catalog.status === "loading" && (
              <div className="mt-8" role="status">
                <p className="font-mono text-sm text-sand/80">Katalog yükleniyor…</p>
                <div aria-hidden="true" className="mt-4 divide-y divide-sand/15 border-y border-sand/15">
                  {variants.map((variant) => (
                    <div key={variant.id} className="flex gap-6 py-6">
                      <span className="w-16 shrink-0 font-mono font-semibold text-sand/70">{variant.label}</span>
                      <div className="w-full space-y-3">
                        <div className="h-4 w-2/3 rounded-sm bg-concrete" />
                        <div className="h-3 w-1/2 rounded-sm bg-concrete" />
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {catalog.status === "error" && (
              <div className="mt-8 rounded-md border border-blood/60 bg-blood/10 px-4 py-6">
                <div role="alert">
                  <p className="font-medium text-parchment">Katalog şu anda yenilenemiyor.</p>
                  <p className="mt-2 max-w-prose text-sm leading-6 text-sand/90">
                    {catalog.offline
                      ? "Cihazınız çevrimdışı görünüyor. Bağlantınızı kontrol edip tekrar deneyin."
                      : "Bağlantı veya katalog hizmeti geçici olarak kullanılamıyor olabilir. Lütfen tekrar deneyin."}
                  </p>
                </div>
                <button type="button" onClick={() => void loadCatalog()} className="btn-rust mt-5">
                  Tekrar dene
                </button>
              </div>
            )}

            {catalog.status === "ready" && (
              <div className="mt-8">
                <p role="status" className="font-mono text-xs leading-6 text-sand/80">
                  {catalog.packages.length === 0
                    ? "Katalogda şu anda Türkçe Wikipedia paketi bulunamadı."
                    : "Katalog yüklendi. Boyutlar katalogda bildirilen değerlerdir."}
                </p>
                <ul className="mt-4 divide-y divide-sand/15 border-y border-sand/15">
                  {variants.map((variant) => {
                    const item = catalog.packages.find((entry) => entry.variant === variant.id)
                    return (
                      <li key={variant.id} className="grid gap-4 py-6 sm:grid-cols-[4rem_minmax(0,1fr)] sm:gap-6">
                        <h3 className="font-mono text-sm font-semibold uppercase tracking-wider text-sand">{variant.label}</h3>
                        <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_auto] md:gap-8">
                          {item ? <>
                            <PackageMetadata item={item} />
                            <div className="md:col-span-2">
                              <button
                                type="button"
                                className={buttonClass}
                                disabled={creating !== null}
                                aria-label={`${variant.label} indir`}
                                onClick={() => void startDownload(item)}
                              >
                                {creating === item.providerPackageId ? "Başlatılıyor…" : "İndir"}
                              </button>
                              {creationError?.id === item.providerPackageId && (
                                <p role="alert" className="mt-3 max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{creationError.message}</p>
                              )}
                            </div>
                          </> : (
                            <p className="text-sm leading-6 text-sand/70">Şu anda katalogda yok</p>
                          )}
                        </div>
                      </li>
                    )
                  })}
                </ul>
              </div>
            )}
          </section>
          )}
        </main>

        <footer className="mt-4 flex flex-wrap items-center justify-between gap-2 font-mono text-[11px] uppercase tracking-[0.14em] text-sand/60">
          <span>Bilginin erişim engeli olmaz</span>
          <span>Yerel arşiv · Kiwix</span>
        </footer>
      </div>
    </div>
  )
}
