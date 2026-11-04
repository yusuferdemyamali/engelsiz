import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { buttonClass, downloadRequest, formatBytes, secondaryButtonClass, type Download } from "./downloads"

export type TranslationDirection = "tr-en" | "en-tr"
export type TranslationModelId = "argos-tr-en" | "argos-en-tr"
export type TranslationModelStatus = "missing" | "ready" | "installing" | "error"

export type TranslationModel = {
  id: TranslationModelId
  source_language: "tr" | "en"
  target_language: "en" | "tr"
  version: string
  size_bytes: number
  status: TranslationModelStatus
  download: Download | null
  error: { code: string; message: string } | null
}

export type TranslationOverview = {
  models: TranslationModel[]
  runtime_available: boolean
  max_characters: number
}

export const MAX_CHARACTERS_FALLBACK = 5000
const POLL_INTERVAL_MS = 1500

const directionOrder: TranslationDirection[] = ["tr-en", "en-tr"]

export function countCharacters(text: string): number {
  return [...text].length
}

export function oppositeDirection(direction: TranslationDirection): TranslationDirection {
  return direction === "tr-en" ? "en-tr" : "tr-en"
}

export function directionLabel(direction: TranslationDirection): string {
  return direction === "tr-en" ? "Türkçe → İngilizce" : "İngilizce → Türkçe"
}

export function modelForDirection(models: TranslationModel[], direction: TranslationDirection): TranslationModel | undefined {
  const source = direction === "tr-en" ? "tr" : "en"
  const target = direction === "tr-en" ? "en" : "tr"
  return models.find((model) => model.source_language === source && model.target_language === target)
}

export function validateTranslationInput(text: string, max: number): string | null {
  if (!text.trim()) return "Çevirmek için önce metin yazın."
  if (countCharacters(text) > max) return `Metin en fazla ${max} karakter olabilir. Metni kısaltıp tekrar deneyin.`
  return null
}

function validOverview(value: TranslationOverview): boolean {
  if (!value || typeof value.runtime_available !== "boolean") return false
  if (!Number.isSafeInteger(value.max_characters) || value.max_characters <= 0) return false
  if (!Array.isArray(value.models) || value.models.length === 0) return false
  return value.models.every((model) =>
    (model.id === "argos-tr-en" || model.id === "argos-en-tr") &&
    (model.source_language === "tr" || model.source_language === "en") &&
    (model.target_language === "tr" || model.target_language === "en") &&
    model.source_language !== model.target_language &&
    typeof model.version === "string" && model.version.length > 0 &&
    Number.isSafeInteger(model.size_bytes) && model.size_bytes > 0 &&
    ["missing", "ready", "installing", "error"].includes(model.status) &&
    (model.download === null || (typeof model.download === "object" && typeof model.download.id === "string")) &&
    (model.error === null || (typeof model.error?.message === "string")),
  )
}

type TranslateRequest = { source: string; target: string; text: string }
type TranslateState =
  | { status: "idle" }
  | { status: "loading"; request: TranslateRequest }
  | { status: "ready"; request: TranslateRequest; translatedText: string }
  | { status: "error"; message: string }

export function useTranslate() {
  const [state, setState] = useState<TranslateState>({ status: "idle" })
  const active = useRef<AbortController | null>(null)

  const cancel = useCallback(() => {
    active.current?.abort()
    active.current = null
  }, [])

  useEffect(() => cancel, [cancel])

  const reset = useCallback(() => {
    cancel()
    setState({ status: "idle" })
  }, [cancel])

  const run = useCallback(async (text: string, source: string, target: string, max: number) => {
    cancel()
    const problem = validateTranslationInput(text, max)
    if (problem || !source || !target) {
      setState(problem ? { status: "error", message: problem } : { status: "idle" })
      return
    }
    const request = { source, target, text }
    const controller = new AbortController()
    active.current = controller
    setState({ status: "loading", request })
    try {
      const result = await downloadRequest<{ translated_text: string }>("/api/translation/translate", controller.signal, {
        source_language: source,
        target_language: target,
        text,
      })
      if (typeof result.translated_text !== "string" || !result.translated_text) {
        throw new Error("Çeviriden geçerli bir yanıt alınamadı. Tekrar deneyin.")
      }
      if (!controller.signal.aborted && active.current === controller) {
        setState({ status: "ready", request, translatedText: result.translated_text })
      }
    } catch (failure) {
      if (!controller.signal.aborted && active.current === controller) {
        setState({ status: "error", message: failure instanceof Error ? failure.message : "Çeviri tamamlanamadı. Tekrar deneyin." })
      }
    } finally {
      if (active.current === controller) active.current = null
    }
  }, [cancel])

  return { state, run, reset }
}

type ModelsState =
  | { status: "loading" }
  | { status: "ready"; overview: TranslationOverview }
  | { status: "error"; message: string }

function downloadActive(download: Download | null): boolean {
  return download?.status === "queued" || download?.status === "downloading"
}

function overviewBusy(overview: TranslationOverview): boolean {
  return overview.models.some((model) => model.status === "installing" || downloadActive(model.download))
}

export type DownloadControlAction = "pause" | "resume" | "cancel" | "retry"

export function downloadControlActions(status: Download["status"]): DownloadControlAction[] {
  if (status === "queued" || status === "downloading") return ["pause", "cancel"]
  if (status === "paused") return ["resume", "cancel"]
  if (status === "failed") return ["retry", "cancel"]
  return []
}

export function extractDownloadPayload(body: unknown): Download | null {
  const download = (body as { download?: unknown } | null)?.download
  return download && typeof (download as Download).id === "string" ? (download as Download) : null
}

export function extractModelPayload(body: unknown): TranslationModel | null {
  const model = (body as { model?: unknown } | null)?.model
  return model && typeof (model as TranslationModel).id === "string" ? (model as TranslationModel) : null
}

export function shouldAutoInstall(model: TranslationModel, initiated: ReadonlySet<string>, attempted: ReadonlySet<string>): boolean {
  const download = model.download
  if (!download || download.status !== "completed") return false
  if (model.status === "ready" || model.status === "installing") return false
  return initiated.has(download.id) && !attempted.has(download.id)
}

export function canTranslateNow(overview: TranslationOverview | null, model: TranslationModel | undefined, text: string, max: number): boolean {
  return !!overview?.runtime_available && model?.status === "ready" && countCharacters(text) <= max && !!text.trim()
}

export function useTranslationModels() {
  const [state, setState] = useState<ModelsState>({ status: "loading" })
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
        const result = await downloadRequest<TranslationOverview>("/api/translation", request.signal)
        if (!validOverview(result)) throw new Error("Çeviri durumu okunamadı. Tekrar deneyin.")
        if (disposed || request.signal.aborted) return
        setState({ status: "ready", overview: result })
        if (overviewBusy(result)) timer = setTimeout(() => void load(), POLL_INTERVAL_MS)
      } catch (failure) {
        if (!disposed && !request.signal.aborted) {
          if (first) setState({ status: "error", message: failure instanceof Error ? failure.message : "Çeviri durumu okunamadı. Tekrar deneyin." })
          else timer = setTimeout(() => void load(), POLL_INTERVAL_MS)
        }
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

  const startDownload = useCallback(async (model: TranslationModel) => {
    const result = await guard<Download>(`${model.id}:download`, "İndirme başlatılıyor…", (signal) =>
      downloadRequest<{ download: Download }>(`/api/translation/models/${encodeURIComponent(model.id)}/download`, signal, {}).then((body) => {
        const download = extractDownloadPayload(body)
        if (!download) throw new Error("İndirme başlatıldı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
        return download
      }))
    if (result) {
      initiated.current.add(result.id)
      refreshQuiet()
    }
  }, [refreshQuiet])

  const installModel = useCallback(async (model: TranslationModel, downloadId: string) => {
    installAttempted.current.add(downloadId)
    const result = await guard<TranslationModel>(`${model.id}:install`, "Kurulum başlatılıyor…", (signal) =>
      downloadRequest<{ model: TranslationModel }>(`/api/translation/models/${encodeURIComponent(model.id)}/install`, signal, { download_id: downloadId }).then((body) => {
        const installed = extractModelPayload(body)
        if (!installed) throw new Error("Kurulum başlatıldı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
        return installed
      }))
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const controlDownload = useCallback(async (model: TranslationModel, action: DownloadControlAction) => {
    const download = model.download
    if (!download) return
    const result = await guard<Download>(`${model.id}:${download.id}:${action}`, "İşleniyor…", (signal) =>
      downloadRequest<{ download: Download }>(`/api/downloads/${encodeURIComponent(download.id)}/${action}`, signal, {}).then((body) => {
        const updated = extractDownloadPayload(body)
        if (!updated) throw new Error("İşlem tamamlandı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
        return updated
      }))
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const overview = state.status === "ready" ? state.overview : null

  useEffect(() => {
    if (!overview) return
    for (const model of overview.models) {
      if (shouldAutoInstall(model, initiated.current, installAttempted.current) && model.download) {
        void installModel(model, model.download.id)
        break
      }
    }
  }, [overview, installModel])

  return { state, overview, pending, actionError, reload, startDownload, installModel, controlDownload }
}

const modelStatusLabels: Record<TranslationModelStatus, string> = {
  missing: "İndirilmedi",
  ready: "Kullanıma hazır",
  installing: "Kuruluyor",
  error: "Kurulum başarısız",
}

const downloadStatusLabels: Record<Download["status"], string> = {
  queued: "Sırada",
  downloading: "İndiriliyor",
  paused: "Duraklatıldı",
  failed: "İndirme başarısız",
  cancelled: "İptal edildi",
  completed: "İndirme tamamlandı",
}

const controlActionLabels: Record<DownloadControlAction, string> = {
  pause: "Duraklat",
  resume: "Devam et",
  cancel: "İptal et",
  retry: "Tekrar dene",
}

export function ModelCard({ model, manager, onOpenDownloads }: {
  model: TranslationModel
  manager: ReturnType<typeof useTranslationModels>
  onOpenDownloads?: () => void
}) {
  const direction: TranslationDirection = model.source_language === "tr" ? "tr-en" : "en-tr"
  const download = model.download
  const active = downloadActive(download)
  const pendingKey = manager.pending?.key ?? ""
  const busy = pendingKey.startsWith(`${model.id}:`)
  const error = manager.actionError?.key.startsWith(`${model.id}:`) ? manager.actionError.message : null
  const percentage = download?.percentage === null || download?.percentage === undefined
    ? null
    : Math.max(0, Math.min(100, download.percentage))
  const autoInstalling = !!download && download.status === "completed" &&
    (model.status === "installing" || (manager.pending?.key === `${model.id}:install`))

  return (
    <li className="space-y-4 py-6" aria-busy={busy}>
      <div className="flex flex-wrap items-center gap-3">
        <h3 className="break-words font-medium text-parchment">{directionLabel(direction)} modeli</h3>
        <span className={`rounded px-2 py-1 font-mono text-xs ${model.status === "ready" ? "status-ok" : model.status === "error" ? "status-bad" : "bg-concrete text-parchment"}`}>
          {modelStatusLabels[model.status]}
        </span>
      </div>
      <p className="font-mono text-xs tabular-nums text-sand/80">{formatBytes(model.size_bytes)} · Sürüm {model.version}</p>

      {model.status === "error" && model.error && (
        <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{model.error.message}</p>
      )}

      {download && model.status !== "ready" && (
        <div className="space-y-2">
          <p role="status" className="inline-flex items-center gap-2 rounded border border-sand/20 bg-coal/60 px-2 py-1 font-mono text-xs uppercase tracking-wider text-parchment">
            {downloadStatusLabels[download.status]}
          </p>
          {(active || download.status === "paused") && (
            <>
              <progress
                className="download-progress"
                max={100}
                value={percentage ?? undefined}
                aria-label={`${directionLabel(direction)} modeli indirme ilerlemesi`}
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

      {error && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{error}</p>}

      <div className="flex flex-wrap gap-3">
        {model.status === "ready" && (
          <p role="status" className="font-mono text-xs leading-6 text-olive">Bu yön çevrimdışı çeviriye hazır.</p>
        )}
        {(model.status === "missing" && !download) || download?.status === "cancelled" || (model.status === "error" && !active) ? (
          <button type="button" className={buttonClass} disabled={busy} onClick={() => void manager.startDownload(model)}>
            {busy ? "Başlatılıyor…" : download?.status === "cancelled" || model.status === "error" ? "Tekrar dene" : "İndir"}
          </button>
        ) : null}
        {download && downloadControlActions(download.status).map((action) => (
          <button
            key={action}
            type="button"
            className={action === "cancel" ? secondaryButtonClass : buttonClass}
            disabled={busy}
            onClick={() => void manager.controlDownload(model, action)}
          >
            {pendingKey.endsWith(`:${action}`) ? "İşleniyor…" : controlActionLabels[action]}
          </button>
        ))}
        {download && active && onOpenDownloads && (
          <button type="button" className={secondaryButtonClass} onClick={onOpenDownloads}>İndirmelerde takip et</button>
        )}
        {download?.status === "completed" && model.status !== "ready" && !autoInstalling && (
          <button type="button" className={buttonClass} disabled={busy} onClick={() => void manager.installModel(model, download.id)}>
            {pendingKey.endsWith(":install") ? "Başlatılıyor…" : "Kur"}
          </button>
        )}
        {autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">İndirme tamamlandı. Model kuruluyor…</p>
        )}
        {model.status === "installing" && !autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">Model kuruluyor… Bu işlem bitene kadar bekleyin.</p>
        )}
      </div>
    </li>
  )
}

export function TranslationView({ onOpenDownloads }: { onOpenDownloads?: () => void }) {
  const models = useTranslationModels()
  const translation = useTranslate()
  const [direction, setDirection] = useState<TranslationDirection>("tr-en")
  const [input, setInput] = useState("")
  const [pasteError, setPasteError] = useState<string | null>(null)
  const [copyFeedback, setCopyFeedback] = useState<"copied" | "failed" | null>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => {
    if (copyTimer.current) clearTimeout(copyTimer.current)
  }, [])

  const overview = models.overview
  const max = overview?.max_characters ?? MAX_CHARACTERS_FALLBACK
  const length = countCharacters(input)
  const overLimit = length > max
  const activeModel = overview ? modelForDirection(overview.models, direction) : undefined
  const otherModel = overview ? modelForDirection(overview.models, oppositeDirection(direction)) : undefined
  const canTranslate = canTranslateNow(overview, activeModel, input, max)
  const translating = translation.state.status === "loading"
  const result = translation.state.status === "ready" ? translation.state.translatedText : ""
  const canPaste = typeof navigator !== "undefined" && !!navigator.clipboard?.readText
  const canCopy = typeof navigator !== "undefined" && !!navigator.clipboard?.writeText

  function swapDirection() {
    setDirection((current) => oppositeDirection(current))
    translation.reset()
    setCopyFeedback(null)
  }

  function clearAll() {
    setInput("")
    translation.reset()
    setPasteError(null)
    setCopyFeedback(null)
    inputRef.current?.focus()
  }

  async function pasteFromClipboard() {
    setPasteError(null)
    try {
      const text = await navigator.clipboard.readText()
      if (typeof text !== "string" || !text) {
        setPasteError("Panoda metin bulunamadı. Metni doğrudan alana yapıştırabilirsiniz.")
        return
      }
      const area = inputRef.current
      if (area && document.activeElement === area) {
        const start = area.selectionStart ?? input.length
        const end = area.selectionEnd ?? input.length
        setInput(input.slice(0, start) + text + input.slice(end))
      } else {
        setInput((current) => (current ? `${current}\n${text}` : text))
      }
      translation.reset()
      inputRef.current?.focus()
    } catch {
      setPasteError("Pano okunamadı. Metni doğrudan alana yapıştırın.")
    }
  }

  async function copyResult() {
    if (!result) return
    setCopyFeedback(null)
    try {
      await navigator.clipboard.writeText(result)
      setCopyFeedback("copied")
      if (copyTimer.current) clearTimeout(copyTimer.current)
      copyTimer.current = setTimeout(() => setCopyFeedback(null), 3000)
    } catch {
      setCopyFeedback("failed")
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!activeModel || translating) return
    void translation.run(input, activeModel.source_language, activeModel.target_language, max)
  }

  function shortcut(event: KeyboardEvent<HTMLTextAreaElement>) {
    if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
      event.preventDefault()
      if (canTranslate && !translating && activeModel) {
        void translation.run(input, activeModel.source_language, activeModel.target_language, max)
      }
    }
  }

  return (
    <section aria-labelledby="translation-heading">
      <p className="terminal-label">06 / Çeviri</p>
      <h2 id="translation-heading" className="mt-2 font-display text-4xl tracking-wide text-parchment">ÇEVİRİ</h2>
      <p className="mt-2 max-w-prose text-sm leading-6 text-sand/90">
        Türkçe ve İngilizce arasında çevrimdışı çeviri yapın. Her yönün modeli ayrı indirilir ve kurulur.
      </p>

      {models.state.status === "loading" && (
        <p role="status" className="mt-8 font-mono text-sm text-sand/80">Çeviri modelleri yükleniyor…</p>
      )}
      {models.state.status === "error" && (
        <div className="mt-8 rounded-md border border-blood/60 bg-blood/10 px-4 py-6">
          <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">{models.state.message}</p>
          <button type="button" className={`${secondaryButtonClass} mt-4`} onClick={models.reload}>Tekrar dene</button>
        </div>
      )}

      {overview && (
        <>
          {!overview.runtime_available && (
            <div className="mt-6 rounded-md border border-blood/60 bg-blood/10 px-4 py-5">
              <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">
                Çeviri motoru bu kurulumda bulunamadı. Uygulamayı yeniden kurun veya güncel sürüme güncelleyin, ardından tekrar deneyin.
              </p>
            </div>
          )}

          <div className="mt-8 flex flex-wrap items-center gap-3" role="group" aria-label="Çeviri yönü">
            <p className="font-mono text-xs uppercase tracking-wider text-sand/70">Yön</p>
            <p aria-live="polite" className="break-words font-medium text-parchment">{directionLabel(direction)}</p>
            <button
              type="button"
              className={secondaryButtonClass}
              aria-label="Çeviri yönünü değiştir"
              onClick={swapDirection}
            >
              Yönü değiştir
            </button>
          </div>

          {activeModel && activeModel.status !== "ready" && otherModel?.status === "ready" && (
            <p role="status" className="mt-4 max-w-prose rounded border border-sand/20 bg-coal/50 px-3 py-2 text-sm leading-6 text-sand/90">
              {directionLabel(oppositeDirection(direction))} hazır. Yönü değiştirerek hemen çevirebilirsiniz.
            </p>
          )}

          <form onSubmit={submit} className="mt-6 space-y-5" aria-label="Metin çevir">
            <div>
              <label htmlFor="translation-input" className="mb-2 block text-sm font-medium text-parchment">Çevrilecek metin</label>
              <textarea
                id="translation-input"
                ref={inputRef}
                rows={6}
                value={input}
                onChange={(event) => {
                  setInput(event.target.value)
                  if (translation.state.status !== "idle") translation.reset()
                  setCopyFeedback(null)
                }}
                onKeyDown={shortcut}
                aria-describedby="translation-help translation-count"
                placeholder={direction === "tr-en" ? "Çevrilecek Türkçe metni yazın" : "Çevrilecek İngilizce metni yazın"}
                className="field-terminal min-h-36 resize-y leading-7"
              />
              <div className="mt-2 flex flex-wrap items-baseline justify-between gap-2">
                <p id="translation-help" className="text-sm leading-6 text-sand/70">
                  {overview && !overview.runtime_available
                    ? "Çeviri motoru bulunamadığı için çeviri yapılamıyor."
                    : activeModel?.status === "ready"
                    ? "Çevirmek için Ctrl+Enter kullanın veya Çevir düğmesini seçin."
                    : "Bu yönü kullanmak için önce modelini indirip kurun."}
                </p>
                <p id="translation-count" role="status" aria-atomic="true" className={`font-mono text-xs tabular-nums ${overLimit ? "font-semibold text-parchment" : "text-sand/70"}`}>
                  {new Intl.NumberFormat("tr-TR").format(length)} / {new Intl.NumberFormat("tr-TR").format(max)} karakter
                </p>
              </div>
              {overLimit && (
                <p role="alert" className="mt-2 max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">
                  Metin en fazla {new Intl.NumberFormat("tr-TR").format(max)} karakter olabilir. Metni kısaltıp tekrar deneyin.
                </p>
              )}
              {pasteError && <p role="alert" className="mt-2 max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{pasteError}</p>}
              <div className="mt-4 flex flex-wrap gap-3">
                <button type="submit" className={buttonClass} disabled={!canTranslate || translating}>
                  {translating ? "Çevriliyor…" : "Çevir"}
                </button>
                {canPaste && (
                  <button type="button" className={secondaryButtonClass} onClick={() => void pasteFromClipboard()}>
                    Panodan yapıştır
                  </button>
                )}
                <button type="button" className={secondaryButtonClass} disabled={!input && translation.state.status === "idle"} onClick={clearAll}>
                  Temizle
                </button>
              </div>
            </div>
          </form>

          <div className="mt-8" aria-busy={translating}>
            <h3 className="text-sm font-medium text-parchment">Çeviri sonucu</h3>
            <div aria-live="polite" aria-atomic="true" className="mt-2 min-h-24 rounded-md border border-sand/20 bg-coal/60 px-4 py-3">
              {translation.state.status === "idle" && (
                <p className="text-sm leading-6 text-sand/70">Çevirdiğiniz metin burada görünecek.</p>
              )}
              {translating && (
                <p role="status" className="font-mono text-sm text-sand/80">Çevriliyor…</p>
              )}
              {translation.state.status === "ready" && (
                <p lang={translation.state.request.target} className="max-w-prose whitespace-pre-wrap break-words text-base leading-7 text-parchment">{result}</p>
              )}
              {translation.state.status === "error" && (
                <div>
                  <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">{translation.state.message}</p>
                  <button
                    type="button"
                    className={`${secondaryButtonClass} mt-3`}
                    disabled={!canTranslate || translating}
                    onClick={() => activeModel && void translation.run(input, activeModel.source_language, activeModel.target_language, max)}
                  >
                    Tekrar dene
                  </button>
                </div>
              )}
            </div>
            {translation.state.status === "ready" && canCopy && (
              <div className="mt-3 flex flex-wrap items-center gap-3">
                <button type="button" className={secondaryButtonClass} onClick={() => void copyResult()}>
                  Sonucu kopyala
                </button>
                {copyFeedback === "copied" && <p role="status" className="font-mono text-xs text-olive">Kopyalandı.</p>}
                {copyFeedback === "failed" && <p role="alert" className="text-sm text-parchment">Kopyalanamadı. Metni seçerek kopyalayın.</p>}
              </div>
            )}
          </div>

          <div className="mt-10">
            <h3 className="font-mono text-xs uppercase tracking-wider text-sand/70">Modeller</h3>
            <p className="mt-1 max-w-prose text-sm leading-6 text-sand/80">
              Her yön bağımsızdır. Bir yön indirilirken diğer yön kuruluysa kullanılabilir.
            </p>
            <ul className="mt-2 divide-y divide-sand/15 border-y border-sand/15">
              {directionOrder.map((entry) => {
                const model = modelForDirection(overview.models, entry)
                return model ? <ModelCard key={model.id} model={model} manager={models} onOpenDownloads={onOpenDownloads} /> : null
              })}
            </ul>
          </div>
        </>
      )}
    </section>
  )
}
