import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { buttonClass, downloadRequest, formatBytes, secondaryButtonClass, type Download } from "./downloads"

export type EducationCategory = "disaster" | "survival" | "science" | "interactive"
export type EducationFormat = "pdf" | "zim" | "html"
export type EducationStatus =
  | "missing"
  | "downloading"
  | "downloaded"
  | "installing"
  | "installed"
  | "failed"
  | "reference"

export type EducationLicense = {
  name: string
  url: string
  attribution: string
  redistribution: string
  verified: boolean
}

export type EducationResource = {
  id: string
  title: string
  description: string
  category: EducationCategory
  source: string
  sourceUrl: string
  language: string
  format: EducationFormat
  sizeBytes: number | null
  version: string
  publishedAt?: string | null
  license: EducationLicense
  downloadUrl?: string | null
  downloadable: boolean
  restriction?: string | null
  status: EducationStatus
  download: Download | null
  error: { code: string; message: string } | null
  openHref: string | null
}

export type EducationListResponse = {
  resources: EducationResource[]
}

export const educationCategories: { id: EducationCategory | "all"; label: string; hint: string }[] = [
  { id: "all", label: "Tümü", hint: "Bütün eğitim içerikleri" },
  { id: "disaster", label: "Afet ve İlk Yardım", hint: "Afet hazırlığı ve ilk yardım" },
  { id: "survival", label: "Hayatta Kalma ve Pratik Bilgiler", hint: "Barınma, su, ateş, yön bulma" },
  { id: "science", label: "Matematik ve Fen", hint: "Matematik ve fen içerikleri" },
  { id: "interactive", label: "İnteraktif Bilim Laboratuvarı", hint: "Etkileşimli HTML dersler" },
]

const categoryLabels: Record<EducationCategory, string> = {
  disaster: "Afet ve İlk Yardım",
  survival: "Hayatta Kalma ve Pratik Bilgiler",
  science: "Matematik ve Fen",
  interactive: "İnteraktif Bilim Laboratuvarı",
}

const formatLabels: Record<EducationFormat, string> = {
  pdf: "PDF",
  zim: "ZIM",
  html: "HTML",
}

const statusLabels: Record<EducationStatus, string> = {
  missing: "İndirilmedi",
  downloading: "İndiriliyor",
  downloaded: "İndirildi",
  installing: "Kuruluyor",
  installed: "Kullanıma hazır",
  failed: "İşlem başarısız",
  reference: "Yalnızca kaynak",
}

const languageLabels: Record<string, string> = {
  tr: "Türkçe",
  en: "İngilizce",
}

export function categoryLabel(category: EducationCategory): string {
  return categoryLabels[category] ?? category
}

export function formatLabel(format: EducationFormat): string {
  return formatLabels[format] ?? format
}

export function statusLabel(status: EducationStatus): string {
  return statusLabels[status] ?? status
}

export function languageLabel(language: string): string {
  if (!language || typeof language !== "string") return "Bilinmiyor"
  return languageLabels[language] ?? language
}

export type DownloadControlAction = "pause" | "resume" | "cancel" | "retry"

export function downloadControlActions(status: Download["status"]): DownloadControlAction[] {
  if (status === "queued" || status === "downloading") return ["pause", "cancel"]
  if (status === "paused") return ["resume", "cancel"]
  if (status === "failed") return ["retry", "cancel"]
  return []
}

const controlActionLabels: Record<DownloadControlAction, string> = {
  pause: "Duraklat",
  resume: "Devam et",
  cancel: "İptal et",
  retry: "Tekrar dene",
}

export function controlActionLabel(action: DownloadControlAction): string {
  return controlActionLabels[action]
}

function downloadActive(download: Download | null | undefined): boolean {
  return download?.status === "queued" || download?.status === "downloading"
}

export function resourceHasActiveWork(resource: EducationResource): boolean {
  if (resource.status === "downloading" || resource.status === "installing") return true
  return downloadActive(resource.download)
}

export function shouldAutoInstallResource(
  resource: EducationResource,
  initiated: ReadonlySet<string>,
  attempted: ReadonlySet<string>,
): boolean {
  const download = resource.download
  if (!download || download.status !== "completed") return false
  if (resource.status === "installed" || resource.status === "installing") return false
  if (resource.status === "reference") return false
  if (!resource.downloadable) return false
  return initiated.has(download.id) && !attempted.has(download.id)
}

export function canOpenResource(resource: EducationResource): boolean {
  return resource.status === "installed" && typeof resource.openHref === "string" && (resource.openHref?.length ?? 0) > 0
}

const MEDICAL_HINT = /ilk yardım|ilkyardım|tıbb|tibb|sağlık|first aid|medical|acil müdahale/

export function isDisasterMedicalMaterial(resource: EducationResource): boolean {
  if (resource.category !== "disaster") return false
  const haystack = `${resource.title ?? ""} ${resource.description ?? ""} ${resource.restriction ?? ""}`.toLocaleLowerCase("tr-TR")
  return MEDICAL_HINT.test(haystack)
}

export function needsSafetyCaveat(resource: EducationResource): boolean {
  return isDisasterMedicalMaterial(resource)
}

export function isReferenceOnly(resource: EducationResource): boolean {
  return resource.status === "reference" || !resource.downloadable
}

function safeLicense(resource: EducationResource): EducationLicense {
  const license = resource.license
  if (!license || typeof license !== "object") {
    return { name: "Bilinmiyor", url: "", attribution: "Bilinmiyor", redistribution: "Bilinmiyor", verified: false }
  }
  return {
    name: typeof license.name === "string" && license.name ? license.name : "Bilinmiyor",
    url: typeof license.url === "string" ? license.url : "",
    attribution: typeof license.attribution === "string" && license.attribution ? license.attribution : "Bilinmiyor",
    redistribution: typeof license.redistribution === "string" && license.redistribution ? license.redistribution : "Bilinmiyor",
    verified: license.verified === true,
  }
}

export function extractResourcePayload(body: unknown): EducationResource | null {
  const resource = (body as { resource?: unknown } | null)?.resource
  if (!resource || typeof resource !== "object") return null
  const candidate = resource as EducationResource
  if (typeof candidate.id !== "string" || candidate.id.length === 0) return null
  if (!validResourceShape(candidate)) return null
  return candidate
}

export function extractDownloadPayload(body: unknown): Download | null {
  const download = (body as { download?: unknown } | null)?.download
  return download != null && typeof download === "object" && typeof (download as Download).id === "string" ? (download as Download) : null
}

const DOWNLOAD_STATUSES: Download["status"][] = ["queued", "downloading", "paused", "failed", "cancelled", "completed"]

function validDownloadShape(download: unknown): boolean {
  if (download === null) return true
  if (!download || typeof download !== "object") return false
  const candidate = download as Download
  if (typeof candidate.id !== "string" || candidate.id.length === 0) return false
  return DOWNLOAD_STATUSES.includes(candidate.status)
}

function validLicenseShape(license: unknown): boolean {
  if (!license || typeof license !== "object") return false
  const candidate = license as EducationLicense
  return (
    typeof candidate.name === "string" &&
    typeof candidate.url === "string" &&
    typeof candidate.attribution === "string" &&
    typeof candidate.redistribution === "string" &&
    typeof candidate.verified === "boolean"
  )
}

function validResourceShape(item: unknown): boolean {
  if (!item || typeof item !== "object") return false
  const candidate = item as EducationResource
  return (
    typeof candidate.id === "string" &&
    candidate.id.length > 0 &&
    typeof candidate.title === "string" &&
    typeof candidate.description === "string" &&
    (candidate.category === "disaster" || candidate.category === "survival" || candidate.category === "science" || candidate.category === "interactive") &&
    (candidate.format === "pdf" || candidate.format === "zim" || candidate.format === "html") &&
    (candidate.status === "missing" ||
      candidate.status === "downloading" ||
      candidate.status === "downloaded" ||
      candidate.status === "installing" ||
      candidate.status === "installed" ||
      candidate.status === "failed" ||
      candidate.status === "reference") &&
    typeof candidate.source === "string" &&
    typeof candidate.sourceUrl === "string" &&
    typeof candidate.language === "string" &&
    (candidate.sizeBytes === null || typeof candidate.sizeBytes === "number") &&
    typeof candidate.version === "string" &&
    typeof candidate.downloadable === "boolean" &&
    validLicenseShape(candidate.license) &&
    validDownloadShape(candidate.download) &&
    (candidate.openHref === null || candidate.openHref === undefined || typeof candidate.openHref === "string") &&
    (candidate.error === null || candidate.error === undefined || (typeof candidate.error === "object" && typeof (candidate.error as { message?: unknown }).message === "string"))
  )
}

export function validEducationList(value: unknown): boolean {
  if (!value || typeof value !== "object") return false
  const resources = (value as EducationListResponse).resources
  return Array.isArray(resources) && resources.every(validResourceShape)
}

export function filterResources(
  resources: EducationResource[],
  category: EducationCategory | "all",
  query: string,
  downloadedOnly: boolean,
): EducationResource[] {
  const normalized = query.trim().toLocaleLowerCase("tr-TR")
  return resources.filter((item) => {
    if (category !== "all" && item.category !== category) return false
    if (downloadedOnly && !(item.status === "installed" || item.status === "downloaded" || canOpenResource(item))) return false
    if (!normalized) return true
    const haystack = `${item.title} ${item.description} ${item.source}`.toLocaleLowerCase("tr-TR")
    return haystack.includes(normalized)
  })
}

export function safetyCaveatDate(resource: EducationResource): string | null {
  if (typeof resource.publishedAt === "string" && resource.publishedAt.length > 0) {
    const date = new Date(resource.publishedAt)
    if (!Number.isNaN(date.getTime())) {
      return new Intl.DateTimeFormat("tr-TR", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" }).format(date)
    }
  }
  return null
}

type ResourcesState =
  | { status: "loading" }
  | { status: "ready"; resources: EducationResource[] }
  | { status: "error"; message: string }

const POLL_INTERVAL_MS = 1000

export function useEducationResources() {
  const [state, setState] = useState<ResourcesState>({ status: "loading" })
  const [refresh, setRefresh] = useState(0)
  const [pending, setPending] = useState<{ key: string; label: string } | null>(null)
  const [actionError, setActionError] = useState<{ key: string; message: string } | null>(null)
  const initiated = useRef<Set<string>>(new Set())
  const installAttempted = useRef<Set<string>>(new Set())
  const actionRequest = useRef<AbortController | null>(null)

  useEffect(() => () => { actionRequest.current?.abort() }, [])

  useEffect(() => {
    if (pending) return
    let disposed = false
    let controller: AbortController | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let first = true

    async function load() {
      if (disposed || document.visibilityState === "hidden" || controller) return
      const request = new AbortController()
      controller = request
      try {
        const result = await downloadRequest<EducationListResponse>("/api/education", request.signal)
        if (controller !== request || disposed || request.signal.aborted) return
        if (!validEducationList(result)) throw new Error("Eğitim listesi okunamadı. Tekrar deneyin.")
        if (controller !== request || disposed || request.signal.aborted) return
        setState({ status: "ready", resources: result.resources })
        if (result.resources.some(resourceHasActiveWork)) {
          timer = setTimeout(() => void load(), POLL_INTERVAL_MS)
        }
      } catch (failure) {
        if (controller !== request || disposed || request.signal.aborted) return
        if (first) setState({ status: "error", message: failure instanceof Error ? failure.message : "Eğitim listesi okunamadı. Tekrar deneyin." })
        else timer = setTimeout(() => void load(), POLL_INTERVAL_MS)
      } finally {
        first = false
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
  }, [pending, refresh])

  const reload = useCallback(() => {
    actionRequest.current?.abort()
    setPending(null)
    setState({ status: "loading" })
    setRefresh((value) => value + 1)
  }, [])

  const refreshQuiet = useCallback(() => setRefresh((value) => value + 1), [])

  async function guard<T>(key: string, label: string, work: (signal: AbortSignal) => Promise<T>): Promise<T | null> {
    if (actionRequest.current) return null
    const controller = new AbortController()
    actionRequest.current = controller
    setPending({ key, label })
    setActionError(null)
    try {
      const result = await work(controller.signal)
      return controller.signal.aborted ? null : result
    } catch (failure) {
      if (!controller.signal.aborted) {
        setActionError({ key, message: failure instanceof Error ? failure.message : "İşlem tamamlanamadı. Tekrar deneyin." })
      }
      return null
    } finally {
      if (!controller.signal.aborted) setPending(null)
      if (actionRequest.current === controller) actionRequest.current = null
    }
  }

  const startDownload = useCallback(async (resource: EducationResource) => {
    const result = await guard<EducationResource>(
      `${resource.id}:download`,
      "İndirme başlatılıyor…",
      (signal) =>
        downloadRequest<{ resource: EducationResource }>(`/api/education/${encodeURIComponent(resource.id)}/download`, signal, {}).then((body) => {
          const next = extractResourcePayload(body)
          if (!next) throw new Error("İndirme başlatıldı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
          return next
        }),
    )
    if (result?.download) initiated.current.add(result.download.id)
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const installResource = useCallback(async (resource: EducationResource, downloadId?: string) => {
    const target = downloadId ?? resource.download?.id ?? ""
    if (target) installAttempted.current.add(target)
    const result = await guard<EducationResource>(
      `${resource.id}:install`,
      "Kurulum başlatılıyor…",
      (signal) =>
        downloadRequest<{ resource: EducationResource }>(
          `/api/education/${encodeURIComponent(resource.id)}/install`,
          signal,
          target ? { download_id: target } : {},
        ).then((body) => {
          const next = extractResourcePayload(body)
          if (!next) throw new Error("Kurulum başlatıldı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
          return next
        }),
    )
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const controlDownload = useCallback(async (resource: EducationResource, action: DownloadControlAction) => {
    const download = resource.download
    if (!download) return
    const result = await guard<Download>(
      `${resource.id}:${download.id}:${action}`,
      "İşleniyor…",
      (signal) =>
        downloadRequest<{ download: Download }>(`/api/downloads/${encodeURIComponent(download.id)}/${action}`, signal, {}).then((body) => {
          const updated = extractDownloadPayload(body)
          if (!updated) throw new Error("İşlem tamamlandı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
          return updated
        }),
    )
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const retryRuntime = useCallback(async () => {
    const result = await guard<unknown>(
      "runtime:retry",
      "İçerik hizmeti yeniden başlatılıyor…",
      (signal) => downloadRequest<unknown>("/api/runtime/retry", signal, {}),
    )
    if (result !== null) refreshQuiet()
  }, [refreshQuiet])

  const removeResource = useCallback(async (resource: EducationResource) => {
    const ok = await guard<boolean>(`${resource.id}:delete`, "Siliniyor…", async (signal) => {
      let raw: Response
      try {
        raw = await fetch(`/api/education/${encodeURIComponent(resource.id)}`, { method: "DELETE", signal, cache: "no-store" })
      } catch {
        throw new Error("Yerel hizmete ulaşılamadı. Uygulamanın çalıştığından emin olup tekrar deneyin.")
      }
      if (!raw.ok) {
        const body = await raw.json().catch(() => null)
        throw new Error(typeof body?.error?.message === "string" ? body.error.message : "Silme tamamlanamadı. Tekrar deneyin.")
      }
      return true
    })
    if (ok) refreshQuiet()
  }, [refreshQuiet])

  const resources = state.status === "ready" ? state.resources : []

  useEffect(() => {
    if (state.status !== "ready") return
    for (const resource of state.resources) {
      if (shouldAutoInstallResource(resource, initiated.current, installAttempted.current) && resource.download) {
        void installResource(resource, resource.download.id)
        break
      }
    }
  }, [state, installResource])

  return { state, resources, pending, actionError, reload, startDownload, installResource, controlDownload, retryRuntime, removeResource }
}

export type EducationManager = ReturnType<typeof useEducationResources>

export function EducationSkeleton() {
  return (
    <div className="mt-8" role="status" aria-label="Eğitim içerikleri yükleniyor">
      <p className="font-mono text-sm text-sand/80">Eğitim içerikleri yükleniyor…</p>
      <ul aria-hidden="true" className="mt-4 divide-y divide-sand/15 border-y border-sand/15">
        {[0, 1, 2].map((index) => (
          <li key={index} className="animate-pulse space-y-4 py-6">
            <div className="flex flex-wrap items-center gap-3">
              <div className="h-5 w-48 rounded-sm bg-concrete" />
              <div className="h-5 w-24 rounded bg-concrete" />
            </div>
            <div className="h-4 w-2/3 rounded-sm bg-concrete" />
            <div className="h-3 w-1/3 rounded-sm bg-concrete" />
            <div className="h-11 w-28 rounded-md bg-concrete" />
          </li>
        ))}
      </ul>
    </div>
  )
}

export function LicenseDetails({ resource }: { resource: EducationResource }) {
  const license = safeLicense(resource)
  return (
    <details className="rounded border border-sand/15 bg-coal/50 px-3 py-2">
      <summary className="cursor-pointer text-sm font-medium text-parchment focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal">
        Lisans ve kaynak
      </summary>
      <div className="mt-2 space-y-1 text-sm leading-6 text-sand/90">
        <p>
          Lisans: {license.name}{" "}
          <span className="font-mono text-xs text-sand/70">{license.verified ? "(doğrulanmış)" : "(doğrulanmadı)"}</span>
        </p>
        <p>Atıf: {license.attribution}</p>
        <p>Yeniden dağıtım: {license.redistribution}</p>
        {license.url && (
          <p className="break-words">
            <a
              href={license.url}
              target="_blank"
              rel="noopener noreferrer"
              className="break-words underline decoration-sand/40 underline-offset-4 hover:decoration-sand focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal"
            >
              Lisans metnini harici sitede aç (internet gerekli, harici bağlantı)
            </a>
          </p>
        )}
        <p>
          Orijinal kaynak: {resource.source || "Bilinmiyor"}
          {resource.sourceUrl && (
            <>
              {" · "}
              <a
                href={resource.sourceUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="break-words underline decoration-sand/40 underline-offset-4 hover:decoration-sand focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal"
              >
                Kaynağı harici sitede aç (internet gerekli, harici bağlantı)
              </a>
            </>
          )}
        </p>
      </div>
    </details>
  )
}

export function ReaderAttribution({ resource }: { resource: EducationResource }) {
  const license = safeLicense(resource)
  const restriction =
    typeof resource.restriction === "string" && resource.restriction.trim().length > 0
      ? resource.restriction.trim()
      : null
  return (
    <div className="mb-4 max-w-prose space-y-1 rounded border border-sand/15 bg-coal/50 px-3 py-2 text-xs leading-5 text-sand/80">
      <p className="break-words">
        <span className="font-medium text-parchment">Lisans: </span>
        {license.url ? (
          <a
            href={license.url}
            target="_blank"
            rel="noopener noreferrer"
            className="break-words underline decoration-sand/40 underline-offset-4 hover:decoration-sand focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal"
          >
            {license.name}
          </a>
        ) : (
          <span className="text-parchment">{license.name}</span>
        )}{" "}
        (harici bağlantı, internet gerekli)
        <span> · Orijinal kaynak: </span>
        {resource.sourceUrl ? (
          <a
            href={resource.sourceUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="break-words underline decoration-sand/40 underline-offset-4 hover:decoration-sand focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal"
          >
            {resource.source || "Bilinmiyor"}
          </a>
        ) : (
          <span>{resource.source || "Bilinmiyor"}</span>
        )}{" "}
        (harici bağlantı, internet gerekli)
      </p>
      <p className="break-words text-sand/90">{license.attribution}</p>
      {restriction && <p className="break-words">Kullanım: {restriction}</p>}
    </div>
  )
}

export function ResourceCard({ resource, manager, onOpen, onOpenPackages }: {
  resource: EducationResource
  manager: EducationManager
  onOpen: (resource: EducationResource) => void
  onOpenPackages?: () => void
}) {
  const [confirming, setConfirming] = useState(false)
  const download = resource.download
  const active = downloadActive(download)
  const pendingKey = manager.pending?.key ?? ""
  const busy = pendingKey.startsWith(`${resource.id}:`) || pendingKey === "runtime:retry"
  const error = manager.actionError?.key.startsWith(`${resource.id}:`) ? manager.actionError.message : null
  const percentage =
    download?.percentage === null || download?.percentage === undefined
      ? null
      : Math.max(0, Math.min(100, download.percentage))
  const openable = canOpenResource(resource)
  const reference = isReferenceOnly(resource)
  const medical = needsSafetyCaveat(resource)
  const restriction = typeof resource.restriction === "string" && resource.restriction.trim().length > 0 ? resource.restriction.trim() : null
  const transferFailed = download?.status === "failed"
  const blockedInstalled = resource.status === "installed" && !openable
  const autoInstalling =
    !!download && download.status === "completed" && (resource.status === "installing" || pendingKey === `${resource.id}:install`)
  const showFreshDownload =
    !reference &&
    !transferFailed &&
    (resource.status === "missing" ||
      download?.status === "cancelled" ||
      (resource.status === "failed" && download?.status !== "completed"))

  return (
    <li className="space-y-4 py-6" aria-busy={busy}>
      <div className="flex flex-wrap items-center gap-3">
        <h3 className="min-w-0 break-words font-medium text-parchment">{resource.title}</h3>
        <span className={`rounded px-2 py-1 font-mono text-xs ${resource.status === "installed" ? "status-ok" : resource.status === "failed" ? "status-bad" : "bg-concrete text-parchment"}`}>
          {statusLabel(resource.status)}
        </span>
        <span className="max-w-full break-words rounded border border-sand/20 bg-coal/60 px-2 py-1 font-mono text-xs uppercase tracking-wider text-sand/70">
          {categoryLabel(resource.category)} · {formatLabel(resource.format)}
        </span>
      </div>
      {resource.description && <p className="max-w-prose break-words text-sm leading-6 text-sand/90">{resource.description}</p>}
      <p className="break-words font-mono text-xs tabular-nums text-sand/80">
        {resource.sizeBytes === null || typeof resource.sizeBytes !== "number" ? "Boyut bilgisi yok" : formatBytes(resource.sizeBytes)} · Sürüm {resource.version} · Dil: {languageLabel(resource.language)} · {resource.source || "Bilinmiyor"}
      </p>
      <LicenseDetails resource={resource} />

      {medical && (
        <p role="note" className="max-w-prose rounded border border-sand/20 bg-coal/50 px-3 py-2 text-sm leading-6 text-sand/90">
          Eğitim amaçlıdır ({safetyCaveatDate(resource) ?? "Yayın tarihi doğrulanmadı"}). Tıbbi öneri yerine geçmez; acil durumda resmi kaynaklara başvurun.
        </p>
      )}
      {!medical && restriction && (
        <p className="max-w-prose rounded border border-sand/20 bg-coal/50 px-3 py-2 text-sm leading-6 text-sand/90">
          Kullanım notu: {restriction}
        </p>
      )}

      {reference && (
        <div className="space-y-2 rounded border border-dashed border-sand/30 px-3 py-3">
          <p role="status" className="max-w-prose text-sm leading-6 text-sand/80">
            Bu kaynak yalnızca referans olarak listelenir; indirme doğrulanmadı.
          </p>
        </div>
      )}

      {blockedInstalled && (
        <div className="space-y-3 rounded border border-blood/60 bg-blood/10 px-3 py-3">
          <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">
            Kurulu içerik şu anda açılamıyor. {resource.error?.message ?? "İçerik hizmeti henüz hazır değil."}
          </p>
          <div className="flex flex-wrap gap-3">
            <button type="button" className={buttonClass} disabled={busy} onClick={() => void manager.retryRuntime()}>
              {pendingKey === "runtime:retry" ? "Yeniden başlatılıyor…" : "İçerik hizmetini tekrar dene"}
            </button>
            {onOpenPackages && (
              <button type="button" className={secondaryButtonClass} onClick={onOpenPackages}>
                Yerel Paketlere git
              </button>
            )}
          </div>
        </div>
      )}

      {download && !reference && (
        <div className="space-y-2">
          <p role="status" className="inline-flex items-center gap-2 rounded border border-sand/20 bg-coal/60 px-2 py-1 font-mono text-xs uppercase tracking-wider text-parchment">
            {download.status === "queued" ? "Sırada" : download.status === "downloading" ? "İndiriliyor" : download.status === "paused" ? "Duraklatıldı" : download.status === "failed" ? "İndirme başarısız" : download.status === "cancelled" ? "İptal edildi" : "İndirme tamamlandı"}
          </p>
          {(active || download.status === "paused") && (
            <>
              <progress
                className="download-progress"
                max={100}
                value={percentage ?? undefined}
                aria-label={`${resource.title} indirme ilerlemesi`}
              />
              <p className="flex flex-wrap justify-between gap-2 font-mono text-xs tabular-nums text-sand/80">
                <span>{formatBytes(download.downloadedBytes)} / {download.totalBytes === null ? "Toplam boyut bilinmiyor" : formatBytes(download.totalBytes)}</span>
                {percentage !== null && <span>%{new Intl.NumberFormat("tr-TR", { maximumFractionDigits: 1 }).format(percentage)}</span>}
              </p>
            </>
          )}
          {download.error && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{download.error.message}</p>}
        </div>
      )}

      {resource.error && !blockedInstalled && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{resource.error.message}</p>}
      {error && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{error}</p>}

      <div className="flex flex-wrap gap-3">
        {openable && resource.openHref && (
          <button type="button" className={buttonClass} onClick={() => onOpen(resource)}>
            İçeriği aç
          </button>
        )}
        {showFreshDownload && (
          <button type="button" className={buttonClass} disabled={busy} onClick={() => void manager.startDownload(resource)}>
            {busy ? "Başlatılıyor…" : resource.status === "failed" ? "Yeniden indir" : "İndir"}
          </button>
        )}
        {download && !reference && downloadControlActions(download.status).map((action) => (
          <button
            key={action}
            type="button"
            className={action === "cancel" ? secondaryButtonClass : buttonClass}
            disabled={busy}
            onClick={() => void manager.controlDownload(resource, action)}
          >
            {pendingKey.endsWith(`:${action}`) ? "İşleniyor…" : controlActionLabel(action)}
          </button>
        ))}
        {resource.status === "failed" && download?.status === "completed" && !reference && !autoInstalling && (
          <button type="button" className={buttonClass} disabled={busy} onClick={() => void manager.installResource(resource)}>
            {pendingKey.endsWith(":install") ? "Başlatılıyor…" : "Kurulumu tekrar dene"}
          </button>
        )}
        {download?.status === "completed" && (resource.status === "downloaded" || resource.status === "downloading" || resource.status === "missing") && !reference && !autoInstalling && (
          <button type="button" className={buttonClass} disabled={busy} onClick={() => void manager.installResource(resource)}>
            {pendingKey.endsWith(":install") ? "Başlatılıyor…" : "Kur"}
          </button>
        )}
        {autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">İndirme tamamlandı. İçerik kuruluyor…</p>
        )}
        {resource.status === "installing" && !autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">İçerik kuruluyor… Bu işlem bitene kadar bekleyin.</p>
        )}
        {(resource.status === "installed" || resource.status === "downloaded") && !reference && (
          confirming ? (
            <span className="flex flex-wrap items-center gap-3">
              <span className="text-sm text-parchment">Kalıcı olarak silinsin mi?</span>
              <button type="button" className={buttonClass} disabled={busy} onClick={() => { setConfirming(false); void manager.removeResource(resource) }}>
                Evet, sil
              </button>
              <button type="button" className={secondaryButtonClass} disabled={busy} onClick={() => setConfirming(false)}>
                Vazgeç
              </button>
            </span>
          ) : (
            <button type="button" className={secondaryButtonClass} disabled={busy} onClick={() => setConfirming(true)}>
              Sil
            </button>
          )
        )}
      </div>
    </li>
  )
}

export function EducationView({ onOpenPackages }: { onOpenPackages?: () => void }) {
  const manager = useEducationResources()
  const [category, setCategory] = useState<EducationCategory | "all">("all")
  const [query, setQuery] = useState("")
  const [downloadedOnly, setDownloadedOnly] = useState(false)
  const [openId, setOpenId] = useState<string | null>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const readerHeading = useRef<HTMLHeadingElement>(null)
  const restoreFocus = useRef(false)
  const offline = typeof navigator !== "undefined" && navigator.onLine === false

  const visible = useMemo(
    () => filterResources(manager.resources, category, query, downloadedOnly),
    [manager.resources, category, query, downloadedOnly],
  )
  const opened = openId ? manager.resources.find((item) => item.id === openId && canOpenResource(item)) ?? null : null

  useEffect(() => {
    if (opened) readerHeading.current?.focus()
    else if (restoreFocus.current) {
      heading.current?.focus()
      restoreFocus.current = false
    }
  }, [opened])

  function openResource(resource: EducationResource) {
    restoreFocus.current = false
    setOpenId(resource.id)
  }

  function returnToList() {
    restoreFocus.current = true
    setOpenId(null)
  }

  return (
    <section aria-labelledby={opened ? "education-reader-heading" : "education-heading"}>
      <div hidden={opened !== null}>
        <p className="terminal-label">07 / Eğitim merkezi</p>
        <h2 ref={heading} id="education-heading" tabIndex={-1} className="mt-2 font-display text-4xl tracking-wide text-parchment focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand">EĞİTİM MERKEZİ</h2>
        <p className="mt-2 max-w-prose text-sm leading-6 text-sand/90">
          Çevrimdışı eğitim içeriklerini indirin ve kurun. Afet ve İlk Yardım, Hayatta Kalma ve Pratik Bilgiler, Matematik ve Fen ile İnteraktif Bilim Laboratuvarı tek arşivde.
        </p>
        {offline && (
          <p role="status" className="mt-4 max-w-prose rounded border border-sand/20 bg-coal/50 px-3 py-2 text-sm leading-6 text-sand/90">
            Çevrimdışı görünüyorsunuz. Yerel liste ve kurulu içerikler açılmaya devam eder; yeni indirmeler bağlantı gerektirir.
          </p>
        )}

        {manager.state.status === "loading" && <EducationSkeleton />}
        {manager.state.status === "error" && (
          <div className="mt-8 rounded-md border border-blood/60 bg-blood/10 px-4 py-6">
            <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">{manager.state.message}</p>
            <button type="button" className={`${secondaryButtonClass} mt-4`} onClick={manager.reload}>Tekrar dene</button>
          </div>
        )}

        {manager.state.status === "ready" && (
          <>
            <div className="mt-8 flex flex-wrap gap-2" role="group" aria-label="Kategori">
              {educationCategories.map((entry) => {
                const selected = category === entry.id
                return (
                  <button
                    key={entry.id}
                    type="button"
                    aria-pressed={selected}
                    title={entry.hint}
                    onClick={() => setCategory(entry.id)}
                    className={`min-h-11 max-w-full break-words rounded-md border px-3 py-2 text-left text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal ${selected ? "border-transparent bg-rust text-parchment" : "border-sand/20 bg-ash/80 text-sand hover:border-sand/40 hover:text-parchment"}`}
                  >
                    {entry.label}
                  </button>
                )
              })}
            </div>

            <div className="mt-6 flex flex-col gap-3 sm:flex-row sm:items-end">
              <div className="min-w-0 flex-1">
                <label htmlFor="education-search" className="mb-2 block text-sm font-medium text-parchment">Ara</label>
                <input
                  id="education-search"
                  type="search"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="Başlık, açıklama veya kaynak yazın"
                  className="field-terminal min-w-0"
                />
              </div>
              <label className="flex min-h-11 cursor-pointer items-center gap-2 text-sm text-sand/90">
                <input
                  type="checkbox"
                  checked={downloadedOnly}
                  onChange={(event) => setDownloadedOnly(event.target.checked)}
                  className="h-4 w-4 accent-[#8B4E2E]"
                />
                Yalnızca indirilenler
              </label>
            </div>

            <p role="status" aria-atomic="true" className="mt-6 font-mono text-xs leading-6 text-sand/80">
              {manager.resources.length === 0
                ? "Henüz eğitim içeriği yok."
                : visible.length === 0
                  ? "Aramanızla eşleşen içerik bulunamadı. Farklı bir kelime veya kategori deneyin."
                  : `${visible.length} içerik listeleniyor.`}
            </p>

            {visible.length > 0 && (
              <ul className="mt-4 divide-y divide-sand/15 border-y border-sand/15">
                {visible.map((resource) => (
                  <ResourceCard key={resource.id} resource={resource} manager={manager} onOpen={openResource} onOpenPackages={onOpenPackages} />
                ))}
              </ul>
            )}
          </>
        )}
      </div>

      {opened?.openHref && (
        <div>
          <div className="mb-5 flex flex-wrap items-center justify-between gap-4">
            <div className="min-w-0">
              <p className="terminal-label break-words">Çevrimdışı içerik · {categoryLabel(opened.category)} · {formatLabel(opened.format)}</p>
              <h2 ref={readerHeading} id="education-reader-heading" tabIndex={-1} className="mt-1 min-w-0 break-words font-display text-3xl tracking-wide text-parchment focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand">{opened.title}</h2>
            </div>
            <button type="button" className={`${secondaryButtonClass} shrink-0`} onClick={returnToList}>Listeye dön</button>
          </div>
          <ReaderAttribution resource={opened} />
          {needsSafetyCaveat(opened) && (
            <p role="note" className="mb-4 max-w-prose break-words rounded border border-sand/20 bg-coal/50 px-3 py-2 text-sm leading-6 text-sand/90">
              Eğitim amaçlıdır ({safetyCaveatDate(opened) ?? "Yayın tarihi doğrulanmadı"}). Tıbbi öneri yerine geçmez.
            </p>
          )}
          {opened.format === "html" ? (
            <iframe
              title={`${opened.title} — Çevrimdışı eğitim içeriği`}
              src={opened.openHref}
              sandbox="allow-scripts"
              className="block h-[75vh] min-h-96 w-full rounded-md border border-sand/25 bg-coal"
            />
          ) : (
            <iframe
              title={`${opened.title} — Çevrimdışı eğitim içeriği`}
              src={opened.openHref}
              className="block h-[75vh] min-h-96 w-full rounded-md border border-sand/25 bg-coal"
            />
          )}
        </div>
      )}
    </section>
  )
}
