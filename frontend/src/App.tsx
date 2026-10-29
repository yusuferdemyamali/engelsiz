import { useCallback, useEffect, useRef, useState } from "react"

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
        <p className="break-words font-medium">{item.title}</p>
        {item.description && (
          <p className="max-w-prose break-words text-sm leading-6 text-muted-foreground">{item.description}</p>
        )}
      </div>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-4 text-sm md:min-w-64">
        <div>
          <dt className="text-muted-foreground">Katalog boyutu</dt>
          <dd className="mt-1 tabular-nums">{formatSize(item.sizeBytes)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Yayın tarihi</dt>
          <dd className="mt-1 tabular-nums">{formatDate(item.publishedAt)}</dd>
        </div>
        {!item.publishedAt && item.updatedAt && (
          <div className="col-span-2">
            <dt className="text-muted-foreground">Katalog güncellemesi</dt>
            <dd className="mt-1 tabular-nums">{formatDate(item.updatedAt)}</dd>
          </div>
        )}
      </dl>
    </>
  )
}

export default function App() {
  const [catalog, setCatalog] = useState<CatalogState>({ status: "loading" })
  const activeRequest = useRef<AbortController | null>(null)

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
    }
  }, [loadCatalog])

  return (
    <main className="mx-auto min-h-screen w-full max-w-5xl px-5 py-8 sm:px-8 sm:py-12">
      <header className="border-b pb-8">
        <h1 className="text-3xl font-bold tracking-tight">Engelsiz</h1>
        <p className="mt-2 text-muted-foreground">Bilginin erişim engeli olmaz.</p>
      </header>

      <section aria-labelledby="catalog-heading" className="pt-8 sm:pt-10">
        <h2 id="catalog-heading" className="text-2xl font-semibold tracking-tight">Bilgi Paketleri</h2>
        <p className="mt-2 text-lg">Türkçe Wikipedia</p>
        <p className="mt-2 max-w-prose text-sm leading-6 text-muted-foreground">
          Kiwix kataloğundaki sürümlerin açıklamalarını, boyutlarını ve yayın tarihlerini inceleyin.
        </p>

        {catalog.status === "loading" && (
          <div className="mt-8" role="status">
            <p className="text-sm text-muted-foreground">Katalog yükleniyor…</p>
            <div aria-hidden="true" className="mt-4 divide-y border-y">
              {variants.map((variant) => (
                <div key={variant.id} className="flex gap-6 py-6">
                  <span className="w-16 shrink-0 font-semibold">{variant.label}</span>
                  <div className="w-full space-y-3">
                    <div className="h-4 w-2/3 rounded-sm bg-muted" />
                    <div className="h-3 w-1/2 rounded-sm bg-muted" />
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {catalog.status === "error" && (
          <div className="mt-8 border-y py-6">
            <div role="alert">
              <p className="font-medium">Katalog şu anda yenilenemiyor.</p>
              <p className="mt-2 max-w-prose text-sm leading-6 text-muted-foreground">
                {catalog.offline
                  ? "Cihazınız çevrimdışı görünüyor. Bağlantınızı kontrol edip tekrar deneyin."
                  : "Bağlantı veya katalog hizmeti geçici olarak kullanılamıyor olabilir. Lütfen tekrar deneyin."}
              </p>
            </div>
            <button
              type="button"
              onClick={() => void loadCatalog()}
              className="mt-5 min-h-11 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 active:bg-primary/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-wait disabled:opacity-60"
            >
              Tekrar dene
            </button>
          </div>
        )}

        {catalog.status === "ready" && (
          <div className="mt-8">
            <p role="status" className="text-sm leading-6 text-muted-foreground">
              {catalog.packages.length === 0
                ? "Katalogda şu anda Türkçe Wikipedia paketi bulunamadı."
                : "Katalog yüklendi. Boyutlar katalogda bildirilen değerlerdir."}
            </p>
            <ul className="mt-4 divide-y border-y">
              {variants.map((variant) => {
                const item = catalog.packages.find((entry) => entry.variant === variant.id)
                return (
                  <li key={variant.id} className="grid gap-4 py-6 sm:grid-cols-[4rem_minmax(0,1fr)] sm:gap-6">
                    <h3 className="font-semibold">{variant.label}</h3>
                    <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_auto] md:gap-8">
                      {item ? <PackageMetadata item={item} /> : (
                        <p className="text-sm leading-6 text-muted-foreground">Şu anda katalogda yok</p>
                      )}
                    </div>
                  </li>
                )
              })}
            </ul>
          </div>
        )}
      </section>
    </main>
  )
}
