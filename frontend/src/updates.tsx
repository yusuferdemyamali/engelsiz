import { useEffect, useRef, useState } from "react"
import { buttonClass, downloadRequest, formatBytes, secondaryButtonClass, variantsLabel, type Download } from "./downloads"
import type { Package } from "./packages"

export type Update = {
  packageId: string
  installedVersion: string
  candidateProviderPackageId: string
  candidateVersion: string
  sizeBytes: number | null
  state: "update_available" | "creating" | "cancelled" | "downloading" | "ready" | "activating" | "succeeded" | "failed" | "interrupted"
  id?: string
  downloadId?: string
  candidatePackageId?: string
  error?: { code: string; message: string } | null
}
export type UpdatesResponse = {
  comparisonAvailable: boolean
  updates: Update[]
  error?: { code: string; message: string } | null
}

export function useUpdates() {
  const [snapshot, setSnapshot] = useState<UpdatesResponse | null>(null)
  const [downloads, setDownloads] = useState<Download[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<{ packageId: string; message: string } | null>(null)
  const [pending, setPending] = useState<string | null>(null)
  const [revision, setRevision] = useState(0)
  const [refresh, setRefresh] = useState(0)
  const action = useRef<AbortController | null>(null)
  const signature = useRef("")

  useEffect(() => () => action.current?.abort(), [])
  useEffect(() => {
    let disposed = false
    let request: AbortController | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let resume = false
    let local = false
    const hidden = () => document.visibilityState === "hidden"
    async function load() {
      if (disposed || hidden() || request) return
      clearTimeout(timer)
      const controller = new AbortController()
      request = controller
      let active = false
      try {
        const result = await downloadRequest<UpdatesResponse>(local ? "/api/updates?local=1" : "/api/updates", controller.signal)
        if (typeof result.comparisonAvailable !== "boolean" || !Array.isArray(result.updates)) throw new Error("Güncelleme bilgileri okunamadı. Tekrar deneyin.")
        if (disposed || controller.signal.aborted) return
        setSnapshot((previous) => local && previous ? { ...previous, updates: [...previous.updates.filter((offer) => offer.state === "update_available" && !result.updates.some((operation) => operation.packageId === offer.packageId && operation.state !== "cancelled")), ...result.updates] } : result)
        setError(null)
        const nextSignature = JSON.stringify(result.updates)
        if (signature.current !== nextSignature) {
          signature.current = nextSignature
          setRevision((value) => value + 1)
        }
        active = result.updates.some((item) => item.state === "creating" || item.state === "downloading" || item.state === "ready" || item.state === "activating")
        if (result.updates.some((item) => item.downloadId)) {
          const transfers = await downloadRequest<{ downloads: Download[] }>("/api/downloads", controller.signal)
          if (!disposed && !controller.signal.aborted && Array.isArray(transfers.downloads)) setDownloads(transfers.downloads)
        }
        local = true
      } catch (failure) {
        if (!disposed && !controller.signal.aborted) setError(failure instanceof Error ? failure.message : "Güncellemeler kontrol edilemedi. Tekrar deneyin.")
      } finally {
        if (request === controller) request = null
        if (!disposed && !controller.signal.aborted) setLoading(false)
        if (!disposed && !hidden() && (resume || active)) {
          timer = setTimeout(() => void load(), resume ? 0 : 2000)
          resume = false
        }
      }
    }
    function visibilityChanged() {
      clearTimeout(timer)
      if (document.visibilityState === "hidden") request?.abort()
      else if (request) resume = true
      else void load()
    }
    document.addEventListener("visibilitychange", visibilityChanged)
    void load()
    return () => {
      disposed = true
      clearTimeout(timer)
      request?.abort()
      document.removeEventListener("visibilitychange", visibilityChanged)
    }
  }, [refresh, pending])

  async function perform(item: Update, activate = false) {
    if (action.current || (activate && !item.id)) return
    const controller = new AbortController()
    action.current = controller
    setPending(item.packageId)
    setActionError(null)
    try {
      await downloadRequest(activate ? `/api/updates/${encodeURIComponent(item.id!)}/activate` : `/api/packages/${encodeURIComponent(item.packageId)}/update`, controller.signal, activate ? {} : { providerPackageId: item.candidateProviderPackageId })
      if (!controller.signal.aborted) setRefresh((value) => value + 1)
    } catch (failure) {
      if (!controller.signal.aborted) setActionError({ packageId: item.packageId, message: failure instanceof Error ? failure.message : "Güncelleme başlatılamadı. Tekrar deneyin." })
    } finally {
      if (!controller.signal.aborted) setPending(null)
      if (action.current === controller) action.current = null
    }
  }
  return { snapshot, downloads, loading, error, actionError, pending, revision, perform, reload: () => setRefresh((value) => value + 1) }
}

type UpdateManager = ReturnType<typeof useUpdates>
const labels: Record<Update["state"], string> = {
  update_available: "Yeni sürüm var",
  creating: "Güncelleme indirmesi hazırlanıyor…",
  cancelled: "Güncelleme iptal edildi. Önceki sürüm korunuyor.",
  downloading: "Güncelleme indiriliyor. Mevcut sürümü kullanmaya devam edebilirsiniz.",
  ready: "Yeni dosya doğrulandı. Kullanıma hazır olduğu kontrol edilecek.",
  activating: "Yeni sürüm hazırlanıyor… İçerik kısa süreliğine kullanılamayabilir.",
  succeeded: "Güncelleme tamamlandı. Önceki sürüm cihazınızda korunuyor.",
  failed: "Güncelleme tamamlanamadı. Önceki sürüm korunuyor.",
  interrupted: "Güncelleme yarım kaldı. Önceki sürüm korunuyor.",
}

export function UpdateCheck({ manager }: { manager: UpdateManager }) {
  return <div className="mt-6 space-y-3 rounded-md border border-sand/20 bg-coal/50 px-4 py-5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h3 className="font-mono text-xs uppercase tracking-wider text-parchment">İçerik güncellemeleri</h3>
      <button type="button" className={secondaryButtonClass} disabled={manager.loading || manager.pending !== null} onClick={manager.reload}>Güncellemeleri kontrol et</button>
    </div>
    <p role="status" aria-atomic="true" className="max-w-prose text-sm leading-6 text-sand/90">
      {manager.loading ? "Yeni sürümler kontrol ediliyor…" : manager.error ? manager.error
        : manager.snapshot?.comparisonAvailable === false ? manager.snapshot.error?.message ?? "Katalog şu anda yenilenemiyor. Güncelleme durumu kontrol edilemedi; yerel içerikleriniz kullanılabilir."
        : manager.snapshot?.updates.some((item) => item.state === "update_available") ? "Yeni sürüm indirmek için ilgili paketin Güncelle düğmesini kullanın."
        : "Katalogda kurulu paketleriniz için daha yeni bir sürüm bulunamadı."}
    </p>
  </div>
}

export function PackageUpdates({ item, packages, manager, onOpenDownloads }: { item: Package; packages: Package[]; manager: UpdateManager; onOpenDownloads?: () => void }) {
  const updates = manager.snapshot?.updates.filter((update) => update.packageId === item.id) ?? []
  return <>
    {updates.map((update) => {
      const download = manager.downloads.find((entry) => entry.id === update.downloadId)
      const candidate = packages.find((entry) => entry.id === update.candidatePackageId || (update.downloadId && entry.downloadId === update.downloadId))
      const candidateInstalled = candidate?.status === "installed"
      const canActivate = !!update.id && candidateInstalled && ["ready", "failed", "interrupted"].includes(update.state)
      const percentage = download?.percentage == null ? null : Math.max(0, Math.min(100, download.percentage))
      return <div key={update.id ?? update.candidateProviderPackageId} className="space-y-3 rounded border border-sand/15 bg-coal/50 px-3 py-4" aria-busy={manager.pending === item.id || update.state === "activating"}>
        <p role="status" aria-atomic="true" className="font-mono text-xs uppercase tracking-wider text-parchment">{labels[update.state]}</p>
        <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-3">
          <div><dt className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Kurulu sürüm</dt><dd className="mt-1 break-words font-mono tabular-nums text-parchment">{update.installedVersion}</dd></div>
          <div><dt className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Yeni sürüm</dt><dd className="mt-1 break-words font-mono tabular-nums text-parchment">{update.candidateVersion}</dd></div>
          <div><dt className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Yeni dosyanın boyutu</dt><dd className="mt-1 font-mono tabular-nums text-parchment">{update.sizeBytes === null ? "Boyut bilgisi yok" : formatBytes(update.sizeBytes)}</dd></div>
        </dl>
        {download && update.state !== "succeeded" && <div className="space-y-2">
          <p className="text-sm leading-6 text-sand/90">{candidate?.status === "verifying" ? "Yeni dosyanın bütünlüğü doğrulanıyor…" : candidate?.status === "installing" ? "Yeni dosya kuruluyor…" : candidate?.status === "failed" ? "Yeni dosyanın kurulumu başarısız. İndirmeler bölümünden tekrar deneyebilirsiniz." : download.status === "completed" && !candidateInstalled ? "İndirme tamamlandı. İndirmeler bölümünden Doğrula ve kur işlemini başlatın." : download.status === "paused" ? "İndirme duraklatıldı." : download.status === "failed" ? "İndirme başarısız. İndirmeler bölümünden tekrar deneyebilirsiniz." : download.status === "cancelled" ? "İndirme iptal edildi. Önceki sürüm korunuyor." : candidateInstalled ? "Yeni dosya doğrulandı ve cihazınızda korunuyor." : "İndirme ilerlemesi"}</p>
          <progress className="download-progress" max={100} value={percentage ?? undefined} aria-label={`${item.title} güncelleme ilerlemesi`} />
          <p className="font-mono text-xs tabular-nums text-sand/80">{formatBytes(download.downloadedBytes)} / {download.totalBytes === null ? "Toplam boyut bilinmiyor" : formatBytes(download.totalBytes)}{percentage === null ? "" : ` · %${new Intl.NumberFormat("tr-TR", { maximumFractionDigits: 1 }).format(percentage)}`}</p>
          {download.error && <p role="alert" className="max-w-prose break-words text-sm leading-6 text-parchment">{download.error.message}</p>}
        </div>}
        {update.error && <p role="alert" className="max-w-prose break-words rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{update.error.message}</p>}
        {manager.actionError?.packageId === item.id && <p role="alert" className="max-w-prose break-words rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{manager.actionError.message}</p>}
        <div className="flex flex-wrap gap-3">
          {update.state === "update_available" && item.active && <button type="button" className={buttonClass} disabled={manager.pending !== null} aria-label={`${item.title} ${variantsLabel(item.variant)}: Güncelle`} onClick={() => void manager.perform(update)}>{manager.pending === item.id ? "Başlatılıyor…" : "Güncelle"}</button>}
          {canActivate && <button type="button" className={buttonClass} disabled={manager.pending !== null} onClick={() => void manager.perform(update, true)}>{manager.pending === item.id ? "Başlatılıyor…" : update.state === "ready" ? "Yeni sürümü etkinleştir" : "Etkinleştirmeyi tekrar dene"}</button>}
          {update.downloadId && update.state !== "succeeded" && onOpenDownloads && <button type="button" className={secondaryButtonClass} onClick={onOpenDownloads}>İndirme ve kurulum işlemlerine git</button>}
        </div>
      </div>
    })}
  </>
}
