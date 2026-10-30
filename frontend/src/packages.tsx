import { useEffect, useRef, useState } from "react"
import { buttonClass, downloadRequest, formatBytes, secondaryButtonClass, variantsLabel } from "./downloads"
import { InstalledContent, RuntimeStatus, useRuntime } from "./runtime"

export type Package = {
  id: string
  downloadId: string
  provider: string
  providerPackageId: string
  contentType: string
  language: string
  variant: string
  title: string
  version: string
  sizeBytes: number
  checksum: { algorithm: "sha256"; value: string }
  status: "verifying" | "installing" | "installed" | "failed"
  error: { code: string; message: string } | null
  createdAt: string
  updatedAt: string
  installedAt: string | null
}

const statusLabels: Record<Package["status"], string> = {
  verifying: "Dosya bütünlüğü doğrulanıyor…",
  installing: "Doğrulama tamamlandı. Paket kuruluyor…",
  installed: "Doğrulandı ve kuruldu",
  failed: "Kurulum başarısız",
}

export function usePackages(refreshKey = 0) {
  const [items, setItems] = useState<Package[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [refresh, setRefresh] = useState(0)
  const [pending, setPending] = useState<string | null>(null)
  const [actionError, setActionError] = useState<{ id: string; message: string } | null>(null)
  const actionRequest = useRef<AbortController | null>(null)

  useEffect(() => () => { actionRequest.current?.abort() }, [])

  useEffect(() => {
    if (pending) return
    let disposed = false
    let controller: AbortController | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let resume = false
    const isHidden = () => document.visibilityState === "hidden"

    async function load() {
      if (disposed || isHidden() || controller) return
      clearTimeout(timer)
      const request = new AbortController()
      controller = request
      let active = false
      try {
        const result = await downloadRequest<{ packages: Package[] }>("/api/packages", request.signal)
        if (!Array.isArray(result.packages)) throw new Error("Paket listesi okunamadı. Tekrar deneyin.")
        if (disposed || request.signal.aborted) return
        setItems(result.packages)
        setError(null)
        active = result.packages.some((item) => item.status === "verifying" || item.status === "installing")
      } catch (failure) {
        if (!disposed && !request.signal.aborted) {
          setError(failure instanceof Error ? failure.message : "Paket listesi okunamadı. Tekrar deneyin.")
        }
      } finally {
        if (controller === request) controller = null
        if (!disposed && !request.signal.aborted) setLoading(false)
        if (!disposed && !isHidden() && (resume || active)) {
          timer = setTimeout(() => void load(), resume ? 0 : 1000)
          resume = false
        }
      }
    }

    function visibilityChanged() {
      clearTimeout(timer)
      if (document.visibilityState === "hidden") {
        resume = false
        controller?.abort()
      } else if (controller) {
        resume = true
      } else {
        void load()
      }
    }

    document.addEventListener("visibilitychange", visibilityChanged)
    void load()
    return () => {
      disposed = true
      clearTimeout(timer)
      controller?.abort()
      document.removeEventListener("visibilitychange", visibilityChanged)
    }
  }, [refresh, refreshKey, pending])

  async function install(downloadId: string) {
    if (actionRequest.current) return
    const controller = new AbortController()
    actionRequest.current = controller
    setPending(downloadId)
    setActionError(null)
    try {
      const result = await downloadRequest<{ package: Package }>(`/api/downloads/${encodeURIComponent(downloadId)}/install`, controller.signal, {})
      if (!controller.signal.aborted) {
        setItems((current) => [...current.filter((item) => item.id !== result.package.id), result.package])
      }
    } catch (failure) {
      if (!controller.signal.aborted) {
        setActionError({ id: downloadId, message: failure instanceof Error ? failure.message : "Kurulum başlatılamadı. Tekrar deneyin." })
      }
    } finally {
      if (!controller.signal.aborted) setPending(null)
      if (actionRequest.current === controller) actionRequest.current = null
    }
  }

  return { items, loading, error, pending, actionError, install, reload: () => setRefresh((value) => value + 1) }
}

export function PackageState({ item, downloadId, title, variant, manager }: {
  item?: Package
  downloadId: string
  title: string
  variant: string
  manager: ReturnType<typeof usePackages>
}) {
  const active = item?.status === "verifying" || item?.status === "installing"
  const pending = manager.pending === downloadId
  const installed = item?.status === "installed"
  const actionError = manager.actionError?.id === downloadId ? manager.actionError.message : null
  return (
    <div className="space-y-3">
      <p role="status" aria-atomic="true" className="text-sm font-medium">
        {pending ? "Kurulum isteği gönderiliyor…" : item ? statusLabels[item.status] : "İndirilen dosya henüz doğrulanıp kurulmadı."}
      </p>
      {installed && <p className="text-sm text-muted-foreground">Kurulu sürüm: {variantsLabel(item.variant)} · {item.version}</p>}
      {item?.error && <p role="alert" className="max-w-prose break-words text-sm leading-6">{item.error.message}</p>}
      {actionError && <p role="alert" className="max-w-prose break-words text-sm leading-6">{actionError}</p>}
      {!installed && (
        <button
          type="button"
          className={buttonClass}
          disabled={manager.pending !== null || active}
          aria-label={`${title} ${variantsLabel(variant)}: ${item?.status === "failed" ? "Kurulumu tekrar dene" : "Doğrula ve kur"}`}
          onClick={() => void manager.install(downloadId)}
        >
          {pending ? "Başlatılıyor…" : active ? "Kurulum sürüyor…" : item?.status === "failed" ? "Kurulumu tekrar dene" : "Doğrula ve kur"}
        </button>
      )}
    </div>
  )
}

export function PackagesView() {
  const manager = usePackages()
  const runtime = useRuntime()
  return (
    <section aria-labelledby="packages-heading" className="pt-8 sm:pt-10">
      <h2 id="packages-heading" className="text-2xl font-semibold tracking-tight">Yerel Paketler</h2>
      <p className="mt-2 max-w-prose text-sm leading-6 text-muted-foreground">
        Kurulumları ve doğrulanmış yerel paketleri çevrimdışıyken de takip edin. Yeni bir kurulum için İndirmeler bölümünü kullanın.
      </p>
      <RuntimeStatus manager={runtime} installedCount={manager.loading ? null : manager.items.filter((item) => item.status === "installed").length} />
      {manager.error && (
        <div className="mt-6 border-y py-5">
          <p role="alert" className="text-sm leading-6">{manager.error}</p>
          <button type="button" className={`${secondaryButtonClass} mt-3`} onClick={manager.reload}>Tekrar dene</button>
        </div>
      )}
      {manager.loading && <p role="status" className="mt-8 text-sm text-muted-foreground">Yerel paketler yükleniyor…</p>}
      {!manager.loading && !manager.error && manager.items.length === 0 && (
        <p role="status" className="mt-8 border-y py-6 text-sm leading-6 text-muted-foreground">
          Henüz yerel paket yok. Tamamlanan bir indirme için Doğrula ve kur düğmesini kullanın.
        </p>
      )}
      {manager.items.length > 0 && (
        <ul className="mt-8 divide-y border-y">
          {manager.items.map((item) => (
            <li key={item.id} className="space-y-4 py-6" aria-busy={manager.pending === item.downloadId}>
              <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2">
                <h3 className="min-w-0 break-words font-medium">{item.title}</h3>
                <p className="break-words text-sm text-muted-foreground">{variantsLabel(item.variant)} · Sürüm {item.version}</p>
              </div>
              <p className="text-sm tabular-nums text-muted-foreground">{formatBytes(item.sizeBytes)}</p>
              <PackageState item={item} downloadId={item.downloadId} title={item.title} variant={item.variant} manager={manager} />
              {item.status === "installed" && <InstalledContent packageId={item.id} title={item.title} variant={variantsLabel(item.variant)} manager={runtime} />}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
