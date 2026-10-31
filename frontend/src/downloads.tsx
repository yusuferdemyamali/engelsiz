import { useEffect, useRef, useState } from "react"
import { PackageState, usePackages } from "./packages"

export type Download = {
  id: string
  provider: string
  providerPackageId: string
  title: string
  variant: string
  fileName: string
  status: "queued" | "downloading" | "paused" | "failed" | "cancelled" | "completed"
  downloadedBytes: number
  totalBytes: number | null
  percentage: number | null
  error: { code: string; message: string } | null
  createdAt: string
  updatedAt: string
}

export const buttonClass = "min-h-11 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 active:bg-primary/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-wait disabled:opacity-60"
export const secondaryButtonClass = "min-h-11 rounded-md border px-4 py-2 text-sm font-medium transition-colors hover:bg-muted active:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-wait disabled:opacity-60"
const numberFormatter = new Intl.NumberFormat("tr-TR", { maximumFractionDigits: 1 })

export function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes < 0) return "Bilinmiyor"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const unit = bytes === 0 ? 0 : Math.min(Math.floor(Math.log(bytes) / Math.log(1000)), units.length - 1)
  return `${numberFormatter.format(bytes / 1000 ** unit)} ${units[unit]}`
}

export async function downloadRequest<T>(path: string, signal: AbortSignal, body?: object): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      signal,
      cache: "no-store",
      ...(body ? { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) } : {}),
    })
  } catch {
    throw new Error("Yerel hizmete ulaşılamadı. Uygulamanın çalıştığından emin olup tekrar deneyin.")
  }
  let result
  try {
    result = await response.json()
  } catch {
    throw new Error("Yerel hizmetten geçerli bir yanıt alınamadı. Tekrar deneyin.")
  }
  if (!response.ok) {
    throw new Error(typeof result?.error?.message === "string" ? result.error.message : "İşlem tamamlanamadı. Tekrar deneyin.")
  }
  return result as T
}

const statusLabels: Record<Download["status"], string> = {
  queued: "Sırada",
  downloading: "İndiriliyor",
  paused: "Duraklatıldı",
  failed: "İndirme başarısız",
  cancelled: "İptal edildi",
  completed: "İndirme tamamlandı",
}

type Action = "pause" | "resume" | "cancel" | "retry"
const actionLabels: Record<Action, string> = {
  pause: "Duraklat",
  resume: "Devam et",
  cancel: "İptal et",
  retry: "Tekrar dene",
}

function eligibleActions(status: Download["status"]): Action[] {
  if (status === "queued" || status === "downloading") return ["pause", "cancel"]
  if (status === "paused") return ["resume", "cancel"]
  if (status === "failed") return ["retry", "cancel"]
  return []
}

export function DownloadsView({ refreshKey }: { refreshKey: number }) {
  const packages = usePackages(refreshKey)
  const [downloads, setDownloads] = useState<Download[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [refresh, setRefresh] = useState(0)
  const [pending, setPending] = useState<{ id: string; action: Action } | null>(null)
  const [actionError, setActionError] = useState<{ id: string; message: string } | null>(null)
  const actionRequest = useRef<AbortController | null>(null)

  useEffect(() => () => { actionRequest.current?.abort() }, [])

  useEffect(() => {
    if (pending) return
    let disposed = false
    let controller: AbortController | null = null
    let timer: ReturnType<typeof setTimeout> | undefined

    async function load() {
      if (disposed || document.visibilityState === "hidden" || controller) return
      const request = new AbortController()
      controller = request
      try {
        const result = await downloadRequest<{ downloads: Download[] }>("/api/downloads", request.signal)
        if (!Array.isArray(result.downloads)) throw new Error("İndirme geçmişi okunamadı. Tekrar deneyin.")
        if (disposed || request.signal.aborted) return
        setDownloads(result.downloads)
        setError(null)
        if (result.downloads.some((item) => item.status === "queued" || item.status === "downloading")) {
          timer = setTimeout(() => void load(), 1000)
        }
      } catch (failure) {
        if (!disposed && !request.signal.aborted) {
          setError(failure instanceof Error ? failure.message : "İndirme geçmişi okunamadı. Tekrar deneyin.")
        }
      } finally {
        if (!disposed && !request.signal.aborted) setLoading(false)
        if (controller === request) controller = null
      }
    }

    function visibilityChanged() {
      clearTimeout(timer)
      controller?.abort()
      controller = null
      if (document.visibilityState !== "hidden") void load()
    }

    document.addEventListener("visibilitychange", visibilityChanged)
    void load()
    return () => {
      disposed = true
      clearTimeout(timer)
      controller?.abort()
      document.removeEventListener("visibilitychange", visibilityChanged)
    }
  }, [pending, refresh, refreshKey])

  async function control(item: Download, action: Action) {
    if (actionRequest.current) return
    const controller = new AbortController()
    actionRequest.current = controller
    setPending({ id: item.id, action })
    setActionError(null)
    try {
      const result = await downloadRequest<{ download: Download }>(`/api/downloads/${encodeURIComponent(item.id)}/${action}`, controller.signal, {})
      if (!controller.signal.aborted) {
        setDownloads((current) => current.map((entry) => entry.id === item.id ? result.download : entry))
      }
    } catch (failure) {
      if (!controller.signal.aborted) {
        setActionError({ id: item.id, message: failure instanceof Error ? failure.message : "İşlem tamamlanamadı. Tekrar deneyin." })
      }
    } finally {
      if (!controller.signal.aborted) setPending(null)
      if (actionRequest.current === controller) actionRequest.current = null
    }
  }

  return (
    <section aria-labelledby="downloads-heading" className="pt-8 sm:pt-10">
      <h2 id="downloads-heading" tabIndex={-1} className="text-2xl font-semibold tracking-tight">İndirmeler</h2>
      <p className="mt-2 max-w-prose text-sm leading-6 text-muted-foreground">
        İndirmeleri takip edin. İndirme tamamlandığında dosyayı doğrulayıp kurabilirsiniz.
      </p>

      {packages.error && (
        <div className="mt-6 border-y py-5">
          <p role="alert" className="text-sm leading-6">Paket durumları yenilenemedi. {packages.error}</p>
          <button type="button" className={`${secondaryButtonClass} mt-3`} onClick={packages.reload}>Paket durumlarını yenile</button>
        </div>
      )}

      {error && (
        <div className="mt-6 border-y py-5">
          <p role="alert" className="text-sm leading-6">{error}</p>
          <button type="button" className={`${secondaryButtonClass} mt-3`} onClick={() => setRefresh((value) => value + 1)} disabled={!!pending}>Tekrar dene</button>
        </div>
      )}
      {loading && <p role="status" className="mt-8 text-sm text-muted-foreground">İndirme geçmişi yükleniyor…</p>}
      {!loading && !error && downloads.length === 0 && (
        <p role="status" className="mt-8 border-y py-6 text-sm leading-6 text-muted-foreground">
          Henüz indirme yok. Bilgi Paketleri bölümünden bir sürüm seçip İndir düğmesini kullanın.
        </p>
      )}
      {downloads.length > 0 && (
        <ul className="mt-8 divide-y border-y">
          {downloads.map((item) => {
            const percentage = item.percentage === null ? null : Math.max(0, Math.min(100, item.percentage))
            const actions = eligibleActions(item.status)
            const localPackage = packages.items.find((entry) => entry.downloadId === item.id)
            return (
              <li key={item.id} className="space-y-4 py-6" aria-busy={pending?.id === item.id}>
                <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2">
                  <h3 className="min-w-0 break-words font-medium">{item.title}</h3>
                  <p className="text-sm text-muted-foreground">{variantsLabel(item.variant)}</p>
                </div>
                <p className="break-all text-sm text-muted-foreground">{item.fileName}</p>
                <p role="status" className="text-sm font-medium">{statusLabels[item.status]}</p>
                <div className="space-y-2">
                  <progress
                    className="download-progress"
                    max={100}
                    value={percentage ?? undefined}
                    aria-label={`${item.title} ${variantsLabel(item.variant)} indirme ilerlemesi`}
                    aria-valuetext={percentage === null ? "Toplam boyut bilinmiyor" : `%${numberFormatter.format(percentage)}`}
                  />
                  <p className="flex flex-wrap justify-between gap-2 text-sm tabular-nums text-muted-foreground">
                    <span>{formatBytes(item.downloadedBytes)} / {item.totalBytes === null ? "Toplam boyut bilinmiyor" : formatBytes(item.totalBytes)}</span>
                    {percentage !== null && <span>%{numberFormatter.format(percentage)}</span>}
                  </p>
                </div>
                {item.error && <p role="alert" className="max-w-prose text-sm leading-6">{item.error.message}</p>}
                {actionError?.id === item.id && <p role="alert" className="max-w-prose text-sm leading-6">{actionError.message}</p>}
                {item.status === "completed" && (
                  <PackageState
                    item={localPackage}
                    downloadId={item.id}
                    title={item.title}
                    variant={item.variant}
                    manager={packages}
                  />
                )}
                {actions.length > 0 && (
                  <div className="flex flex-wrap gap-3">
                    {actions.map((action) => (
                      <button
                        key={action}
                        type="button"
                        className={action === "cancel" ? secondaryButtonClass : buttonClass}
                        disabled={!!pending}
                        aria-label={`${variantsLabel(item.variant)}: ${actionLabels[action]}`}
                        onClick={() => void control(item, action)}
                      >
                        {pending?.id === item.id && pending.action === action ? "İşleniyor…" : actionLabels[action]}
                      </button>
                    ))}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

export function variantsLabel(variant: string) {
  return ({ mini: "Mini", nopic: "Nopic", maxi: "Maxi" } as Record<string, string>)[variant] ?? variant
}
