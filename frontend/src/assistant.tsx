import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { buttonClass, downloadRequest, formatBytes, secondaryButtonClass, type Download } from "./downloads"

export type AssistantProfileId = "lite" | "balanced" | "quality"
export type AssistantProfileStatus = "missing" | "downloading" | "downloaded" | "installing" | "ready" | "error"
export type AssistantRuntimeStatus =
  | "missing"
  | "downloading"
  | "downloaded"
  | "installing"
  | "ready"
  | "error"
  | "unavailable"

export type AssistantProfile = {
  id: string
  title: string
  model_name: string
  quantization: string
  size_bytes: number
  ram_gb: number
  context_size: number
  license: string
  source: string
  status: AssistantProfileStatus
  download: Download | null
  error: { code: string; message: string } | null
}

export type AssistantRuntime = {
  available: boolean
  status: AssistantRuntimeStatus
  download: Download | null
  error: { code: string; message: string } | null
  version: string
}

export type AssistantHardware = {
  total_memory_bytes: number
  available_memory_bytes: number
  cpu_count: number
}

export type AssistantStatus = {
  profiles: AssistantProfile[]
  active_profile_id: string
  recommended_profile_id: string
  hardware: AssistantHardware
  runtime: AssistantRuntime
  busy: boolean
}

export type ChatSource = {
  id: string
  title: string
  href: string
  excerpt: string
}

export const MAX_PROMPT_CHARS = 2000
const POLL_INTERVAL_MS = 1500
const PROFILE_ORDER = ["lite", "balanced", "quality"] as const

export function countCharacters(text: string): number {
  return [...text].length
}

export function validatePromptInput(text: string, max: number): string | null {
  if (!text.trim()) return "Sormak için önce bir soru yazın."
  if (countCharacters(text) > max) return `Soru en fazla ${max} karakter olabilir. Kısaltıp tekrar deneyin.`
  return null
}

export function isLocalSourceHref(href: string): boolean {
  if (typeof href !== "string" || href.length === 0) return false
  if (!href.startsWith("/content/") && !href.startsWith("/education/content/")) return false
  if (/[\\\u0000-\u0020\u007f?#]/u.test(href)) return false
  // Encoded separators can change routing after the server decodes the path.
  // Keep normal percent-encoded Turkish article titles, not encoded slashes.
  if (/%(?:2f|5c|0[0-9a-f]|1[0-9a-f]|7f)/iu.test(href)) return false
  try {
    const url = new URL(href, "http://engelsiz.invalid")
    if (url.origin !== "http://engelsiz.invalid") return false
    return url.pathname === href || url.pathname.startsWith("/content/") || url.pathname.startsWith("/education/content/")
  } catch {
    return false
  }
}

function validDownloadShape(download: unknown): boolean {
  if (download === null) return true
  if (!download || typeof download !== "object") return false
  const candidate = download as Download
  if (typeof candidate.id !== "string" || candidate.id.length === 0) return false
  return ["queued", "downloading", "paused", "failed", "cancelled", "completed"].includes(candidate.status)
}

const PROFILE_STATUSES: AssistantProfileStatus[] = ["missing", "downloading", "downloaded", "installing", "ready", "error"]
const RUNTIME_STATUSES: AssistantRuntimeStatus[] = ["missing", "downloading", "downloaded", "installing", "ready", "error", "unavailable"]

function validProfileShape(item: unknown): boolean {
  if (!item || typeof item !== "object") return false
  const candidate = item as AssistantProfile
  return (
    typeof candidate.id === "string" &&
    candidate.id.length > 0 &&
    typeof candidate.title === "string" &&
    candidate.title.length > 0 &&
    typeof candidate.model_name === "string" &&
    typeof candidate.quantization === "string" &&
    Number.isSafeInteger(candidate.size_bytes) &&
    candidate.size_bytes > 0 &&
    typeof candidate.ram_gb === "number" &&
    Number.isFinite(candidate.ram_gb) &&
    candidate.ram_gb > 0 &&
    Number.isSafeInteger(candidate.context_size) &&
    candidate.context_size > 0 &&
    typeof candidate.license === "string" &&
    typeof candidate.source === "string" &&
    PROFILE_STATUSES.includes(candidate.status) &&
    validDownloadShape(candidate.download) &&
    (candidate.error === null || (typeof candidate.error === "object" && typeof (candidate.error as { message?: unknown }).message === "string"))
  )
}

export function validAssistantStatus(value: unknown): boolean {
  if (!value || typeof value !== "object") return false
  const candidate = value as AssistantStatus
  if (!Array.isArray(candidate.profiles) || candidate.profiles.length === 0) return false
  if (!candidate.profiles.every(validProfileShape)) return false
  if (typeof candidate.active_profile_id !== "string") return false
  if (typeof candidate.recommended_profile_id !== "string") return false
  if (typeof candidate.busy !== "boolean") return false
  const hardware = candidate.hardware
  if (!hardware || typeof hardware !== "object") return false
  if (!Number.isSafeInteger(hardware.total_memory_bytes) || !Number.isSafeInteger(hardware.available_memory_bytes)) return false
  if (!Number.isSafeInteger(hardware.cpu_count) || hardware.cpu_count <= 0) return false
  const runtime = candidate.runtime
  if (!runtime || typeof runtime !== "object") return false
  if (typeof runtime.available !== "boolean") return false
  if (!RUNTIME_STATUSES.includes(runtime.status)) return false
  if (typeof runtime.version !== "string") return false
  if (!validDownloadShape(runtime.download)) return false
  if (!(runtime.error === null || (typeof runtime.error === "object" && typeof runtime.error.message === "string"))) return false
  return true
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

export function extractProfilePayload(body: unknown): AssistantProfile | null {
  const profile = (body as { profile?: unknown } | null)?.profile
  return profile && typeof (profile as AssistantProfile).id === "string" ? (profile as AssistantProfile) : null
}

export function shouldAutoInstallProfile(
  profile: AssistantProfile,
  initiated: ReadonlySet<string>,
  attempted: ReadonlySet<string>,
): boolean {
  const download = profile.download
  if (!download || download.status !== "completed") return false
  if (profile.status === "ready" || profile.status === "installing") return false
  return initiated.has(download.id) && !attempted.has(download.id)
}

export function shouldAutoInstallRuntime(
  runtime: AssistantRuntime,
  initiated: ReadonlySet<string>,
  attempted: ReadonlySet<string>,
): boolean {
  const download = runtime.download
  if (!download || download.status !== "completed") return false
  if (runtime.status === "ready" || runtime.status === "installing") return false
  return initiated.has(download.id) && !attempted.has(download.id)
}

function downloadActive(download: Download | null): boolean {
  return download?.status === "queued" || download?.status === "downloading"
}

function statusBusy(status: AssistantStatus): boolean {
  if (status.busy) return true
  if (status.profiles.some((profile) => profile.status === "installing" || downloadActive(profile.download))) return true
  return status.runtime.status === "installing" || downloadActive(status.runtime.download)
}

export function activeProfile(status: AssistantStatus | null): AssistantProfile | undefined {
  if (!status || !status.active_profile_id) return undefined
  return status.profiles.find((profile) => profile.id === status.active_profile_id && profile.status === "ready")
}

export function canChatNow(status: AssistantStatus | null, profile: AssistantProfile | undefined, text: string): boolean {
  if (!status?.runtime.available) return false
  if (status.busy) return false
  if (!profile || profile.status !== "ready") return false
  if (!text.trim()) return false
  if (countCharacters(text) > MAX_PROMPT_CHARS) return false
  return true
}

export function orderedProfiles(profiles: AssistantProfile[]): AssistantProfile[] {
  return [...profiles].sort((a, b) => {
    const ai = PROFILE_ORDER.indexOf(a.id as (typeof PROFILE_ORDER)[number])
    const bi = PROFILE_ORDER.indexOf(b.id as (typeof PROFILE_ORDER)[number])
    return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi)
  })
}

type AssistantState =
  | { status: "loading" }
  | { status: "ready"; snapshot: AssistantStatus }
  | { status: "error"; message: string }

export function useAssistantStatus() {
  const [state, setState] = useState<AssistantState>({ status: "loading" })
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
        const result = await downloadRequest<AssistantStatus>("/api/assistant", request.signal)
        if (!validAssistantStatus(result)) throw new Error("Asistan durumu okunamadı. Tekrar deneyin.")
        if (disposed || request.signal.aborted) return
        setState({ status: "ready", snapshot: result })
        if (statusBusy(result)) timer = setTimeout(() => void load(), POLL_INTERVAL_MS)
      } catch (failure) {
        if (!disposed && !request.signal.aborted) {
          if (first) setState({ status: "error", message: failure instanceof Error ? failure.message : "Asistan durumu okunamadı. Tekrar deneyin." })
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

  const startDownload = useCallback(async (profile: AssistantProfile) => {
    const result = await guard<Download>(`${profile.id}:download`, "İndirme başlatılıyor…", (signal) =>
      downloadRequest<{ download: Download }>(`/api/assistant/profiles/${encodeURIComponent(profile.id)}/download`, signal, {}).then((body) => {
        const download = extractDownloadPayload(body)
        if (!download) throw new Error("İndirme başlatıldı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
        return download
      }))
    if (result) {
      initiated.current.add(result.id)
      refreshQuiet()
    }
  }, [refreshQuiet])

  const installProfile = useCallback(async (profile: AssistantProfile, downloadId: string) => {
    installAttempted.current.add(downloadId)
    const result = await guard<AssistantProfile>(`${profile.id}:install`, "Kurulum başlatılıyor…", (signal) =>
      downloadRequest<{ profile: AssistantProfile }>(`/api/assistant/profiles/${encodeURIComponent(profile.id)}/install`, signal, { download_id: downloadId }).then((body) => {
        const next = extractProfilePayload(body)
        if (!next) throw new Error("Kurulum başlatıldı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
        return next
      }))
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const activateProfile = useCallback(async (profile: AssistantProfile) => {
    const result = await guard<true>(`${profile.id}:activate`, "Etkinleştiriliyor…", async (signal) => {
      await downloadRequest<Record<string, never>>(`/api/assistant/profiles/${encodeURIComponent(profile.id)}/activate`, signal, {})
      return true
    })
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const removeProfile = useCallback(async (profile: AssistantProfile) => {
    const result = await guard<boolean>(`${profile.id}:delete`, "Siliniyor…", async (signal) => {
      let raw: Response
      try {
        raw = await fetch(`/api/assistant/profiles/${encodeURIComponent(profile.id)}`, { method: "DELETE", signal, cache: "no-store" })
      } catch {
        throw new Error("Yerel hizmete ulaşılamadı. Uygulamanın çalıştığından emin olup tekrar deneyin.")
      }
      if (!raw.ok) {
        const body = await raw.json().catch(() => null)
        throw new Error(typeof body?.error?.message === "string" ? body.error.message : "Silme tamamlanamadı. Tekrar deneyin.")
      }
      return true
    })
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const controlDownload = useCallback(async (key: string, download: Download | null, action: DownloadControlAction) => {
    if (!download) return
    const result = await guard<Download>(`${key}:${download.id}:${action}`, "İşleniyor…", (signal) =>
      downloadRequest<{ download: Download }>(`/api/downloads/${encodeURIComponent(download.id)}/${action}`, signal, {}).then((body) => {
        const updated = extractDownloadPayload(body)
        if (!updated) throw new Error("İşlem tamamlandı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
        return updated
      }))
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const startRuntimeDownload = useCallback(async () => {
    const result = await guard<Download>("runtime:download", "Motor indiriliyor…", (signal) =>
      downloadRequest<{ download: Download }>("/api/assistant/runtime/download", signal, {}).then((body) => {
        const download = extractDownloadPayload(body)
        if (!download) throw new Error("İndirme başlatıldı ancak geçerli bir yanıt alınamadı. Tekrar deneyin.")
        return download
      }))
    if (result) {
      initiated.current.add(result.id)
      refreshQuiet()
    }
  }, [refreshQuiet])

  const installRuntime = useCallback(async (downloadId: string) => {
    installAttempted.current.add(downloadId)
    const result = await guard<true>("runtime:install", "Motor kuruluyor…", async (signal) => {
      await downloadRequest<Record<string, never>>("/api/assistant/runtime/install", signal, { download_id: downloadId })
      return true
    })
    if (result) refreshQuiet()
  }, [refreshQuiet])

  const snapshot = state.status === "ready" ? state.snapshot : null

  useEffect(() => {
    if (!snapshot) return
    for (const profile of snapshot.profiles) {
      if (shouldAutoInstallProfile(profile, initiated.current, installAttempted.current) && profile.download) {
        void installProfile(profile, profile.download.id)
        return
      }
    }
    if (shouldAutoInstallRuntime(snapshot.runtime, initiated.current, installAttempted.current) && snapshot.runtime.download) {
      void installRuntime(snapshot.runtime.download.id)
    }
  }, [snapshot, installProfile, installRuntime])

  return {
    state,
    snapshot,
    pending,
    actionError,
    reload,
    startDownload,
    installProfile,
    activateProfile,
    removeProfile,
    controlDownload,
    startRuntimeDownload,
    installRuntime,
  }
}

export type AssistantManager = ReturnType<typeof useAssistantStatus>

export type SseParsed =
  | { event: "status"; state: "searching" | "loading" | "generating" }
  | { event: "sources"; sources: ChatSource[] }
  | { event: "delta"; text: string }
  | { event: "done"; grounded: boolean }
  | { event: "error"; message: string }

export function parseSseBlock(block: string): SseParsed | null {
  const lines = block.split("\n").map((line) => line.trimEnd()).filter((line) => line.length > 0 && !line.startsWith(":"))
  if (lines.length === 0) return null
  let event = ""
  const dataLines: string[] = []
  for (const line of lines) {
    if (line.startsWith("event:")) event = line.slice("event:".length).trim()
    else if (line.startsWith("data:")) dataLines.push(line.slice("data:".length).trimStart())
  }
  if (dataLines.length === 0) return null
  const raw = dataLines.join("\n")
  let data: unknown
  try {
    data = JSON.parse(raw)
  } catch {
    return null
  }
  if (!data || typeof data !== "object") return null
  const record = data as Record<string, unknown>
  if (event === "status" && typeof record.state === "string" && ["searching", "loading", "generating"].includes(record.state)) {
    return { event: "status", state: record.state as "searching" | "loading" | "generating" }
  }
  if (event === "sources" && Array.isArray(record.sources)) {
    const sources: ChatSource[] = []
    for (const entry of record.sources.slice(0, 5)) {
      if (!entry || typeof entry !== "object") continue
      const candidate = entry as { id?: unknown; title?: unknown; href?: unknown; excerpt?: unknown }
      if (typeof candidate.id !== "string" || typeof candidate.title !== "string" || typeof candidate.href !== "string") continue
      if (!isLocalSourceHref(candidate.href)) continue
      sources.push({
        id: candidate.id,
        title: candidate.title,
        href: candidate.href,
        excerpt: typeof candidate.excerpt === "string" ? candidate.excerpt.slice(0, 2000) : "",
      })
    }
    return { event: "sources", sources }
  }
  if (event === "delta" && typeof record.text === "string") {
    return { event: "delta", text: record.text }
  }
  if (event === "done") {
    return { event: "done", grounded: record.grounded === true }
  }
  if (event === "error") {
    const nested = record.error as { message?: unknown } | undefined
    const message =
      typeof nested?.message === "string" && nested.message.length > 0
        ? nested.message
        : typeof record.message === "string" && record.message.length > 0
          ? (record.message as string)
          : "Yanıt alınamadı. Tekrar deneyin."
    return { event: "error", message }
  }
  return null
}

export type ChatTurn = {
  id: number
  prompt: string
  profileId: string
  text: string
  sources: ChatSource[]
  grounded: boolean
  interrupted: boolean
  status: "streaming" | "ready" | "error"
  phase?: "searching" | "loading" | "generating"
  message?: string
}

export type ChatState =
  | { status: "idle" }
  | { status: "streaming"; phase: "searching" | "loading" | "generating"; text: string; sources: ChatSource[] }
  | { status: "ready"; text: string; sources: ChatSource[]; grounded: boolean; interrupted: boolean; profileId: string; prompt: string }
  | { status: "error"; message: string; partialText: string; sources: ChatSource[]; profileId: string; prompt: string }

const MAX_SSE_BUFFER = 262144
const MAX_ANSWER_CHARS = 20000

export function useAssistantChat() {
  const [turns, setTurns] = useState<ChatTurn[]>([])
  const active = useRef<AbortController | null>(null)
  const requestId = useRef(0)
  const turnSeq = useRef(0)

  const stop = useCallback((markInterrupted: boolean) => {
    const controller = active.current
    active.current = null
    requestId.current += 1
    if (controller) controller.abort()
    if (markInterrupted) {
      setTurns((previous) =>
        previous.map((turn) => {
          if (turn.status !== "streaming") return turn
          if (!turn.text && turn.sources.length === 0) {
            return { ...turn, status: "error" as const, message: "Durduruldu. Yanıt alınamadı." }
          }
          return { ...turn, status: "ready" as const, grounded: turn.sources.length > 0, interrupted: true }
        }),
      )
    }
  }, [])

  useEffect(() => () => {
    active.current?.abort()
    active.current = null
  }, [])

  const reset = useCallback(() => {
    active.current?.abort()
    active.current = null
    requestId.current += 1
    setTurns([])
  }, [])

  const run = useCallback(async (prompt: string, profileId: string) => {
    active.current?.abort()
    active.current = null
    const id = requestId.current + 1
    requestId.current = id
    const problem = validatePromptInput(prompt, MAX_PROMPT_CHARS)
    if (problem || !profileId) {
      if (!problem) return
      const failedId = turnSeq.current + 1
      turnSeq.current = failedId
      setTurns((previous) => [
        ...previous,
        { id: failedId, prompt, profileId, text: "", sources: [], grounded: false, interrupted: false, status: "error", message: problem },
      ])
      return
    }
    const trimmed = prompt.trim()
    const controller = new AbortController()
    active.current = controller
    const turnId = turnSeq.current + 1
    turnSeq.current = turnId
    setTurns((previous) => [
      ...previous,
      { id: turnId, prompt: trimmed, profileId, text: "", sources: [], grounded: false, interrupted: false, status: "streaming", phase: "searching" },
    ])
    let text = ""
    let sources: ChatSource[] = []
    let phase: "searching" | "loading" | "generating" = "searching"
    let grounded = false
    let sawDone = false

    function current(): boolean {
      return requestId.current === id && active.current === controller && !controller.signal.aborted
    }

    function patch(patchTurn: Partial<ChatTurn>) {
      setTurns((previous) => previous.map((turn) => (turn.id === turnId ? { ...turn, ...patchTurn } : turn)))
    }

    function normalize(chunk: string): string {
      return chunk.replace(/\r\n?/g, "\n")
    }

    try {
      let response: Response
      try {
        response = await fetch("/api/assistant/chat", {
          method: "POST",
          headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
          body: JSON.stringify({ profile_id: profileId, prompt: trimmed }),
          signal: controller.signal,
          cache: "no-store",
        })
      } catch (failure) {
        if (controller.signal.aborted || requestId.current !== id) return
        throw new Error(failure instanceof Error && failure.name === "AbortError" ? "abort" : "Yerel hizmete ulaşılamadı. Uygulamanın çalıştığından emin olup tekrar deneyin.")
      }
      if (!response.ok) {
        const body = await response.json().catch(() => null)
        const message =
          typeof body?.error?.message === "string" && body.error.message.length > 0
            ? body.error.message
            : "Yanıt alınamadı. Tekrar deneyin."
        throw new Error(message)
      }
      const contentType = response.headers.get("content-type") ?? ""
      if (!contentType.includes("text/event-stream")) {
        throw new Error("Yerel hizmetten geçerli bir yanıt alınamadı. Tekrar deneyin.")
      }
      const reader = response.body?.getReader()
      if (!reader) throw new Error("Yerel hizmetten geçerli bir yanıt alınamadı. Tekrar deneyin.")
      const decoder = new TextDecoder("utf-8")
      let buffer = ""
      for (;;) {
        const { done, value } = await reader.read()
        if (!current()) {
          try { await reader.cancel() } catch { /* yoksay */ }
          return
        }
        if (done) break
        buffer += normalize(decoder.decode(value, { stream: true }))
        if (buffer.length > MAX_SSE_BUFFER) {
          throw new Error("Yanıt alınamadı. Tekrar deneyin.")
        }
        const blocks = buffer.split("\n\n")
        buffer = blocks.pop() ?? ""
        for (const block of blocks) {
          const parsed = parseSseBlock(block)
          if (!parsed || !current()) continue
          if (parsed.event === "status") {
            phase = parsed.state
            patch({ phase, text, sources: [...sources] })
          } else if (parsed.event === "sources") {
            sources = parsed.sources
            patch({ text, sources: [...sources] })
          } else if (parsed.event === "delta") {
            text += parsed.text
            if (text.length > MAX_ANSWER_CHARS) text = text.slice(0, MAX_ANSWER_CHARS)
            patch({ text, sources: [...sources] })
          } else if (parsed.event === "done") {
            grounded = parsed.grounded
            sawDone = true
          } else if (parsed.event === "error") {
            throw new Error(parsed.message)
          }
        }
      }
      buffer += normalize(decoder.decode())
      if (buffer.length > MAX_SSE_BUFFER) {
        throw new Error("Yanıt alınamadı. Tekrar deneyin.")
      }
      for (const block of buffer.split("\n\n")) {
        const candidate = block.trim()
        if (!candidate || !current()) continue
        const parsed = parseSseBlock(candidate)
        if (!parsed) continue
        if (parsed.event === "delta") {
          text += parsed.text
          if (text.length > MAX_ANSWER_CHARS) text = text.slice(0, MAX_ANSWER_CHARS)
        } else if (parsed.event === "sources") {
          sources = parsed.sources
        } else if (parsed.event === "done") {
          grounded = parsed.grounded
          sawDone = true
        } else if (parsed.event === "error") {
          throw new Error(parsed.message)
        } else if (parsed.event === "status") {
          phase = parsed.state
        }
      }
      patch({ text, sources: [...sources], phase })
      if (!current()) return
      if (!sawDone) {
        if (text.trim() || sources.length > 0) {
          patch({ status: "ready", text: text || "Yanıt yarıda kesildi.", sources: [...sources], grounded, interrupted: true })
        } else {
          patch({ status: "error", message: "Yanıt yarıda kesildi. Tekrar deneyin." })
        }
        return
      }
      if (!text.trim() && sources.length === 0 && !grounded) {
        patch({
          status: "ready",
          text: "Yerel içerikte bu soruya dair kanıt bulunamadı. Farklı bir soru deneyin ya da ilgili paketi kurun.",
          sources: [],
          grounded: false,
          interrupted: false,
        })
      } else {
        patch({ status: "ready", text, sources: [...sources], grounded, interrupted: false })
      }
    } catch (failure) {
      if (!current() && (controller.signal.aborted || requestId.current !== id)) return
      if (failure instanceof Error && failure.message === "abort") return
      const message = failure instanceof Error ? failure.message : "Yanıt alınamadı. Tekrar deneyin."
      if (text.trim() || sources.length > 0) {
        patch({
          status: "ready",
          text: text || "Yanıt yarıda kesildi.",
          sources: [...sources],
          grounded,
          interrupted: true,
        })
      } else {
        patch({ status: "error", message })
      }
    } finally {
      if (active.current === controller) active.current = null
    }
  }, [])

  const live = turns.find((turn) => turn.status === "streaming") ?? null
  const streaming = live !== null
  const state: ChatState = live
    ? { status: "streaming", phase: live.phase ?? "searching", text: live.text, sources: live.sources }
    : { status: "idle" }

  return { turns, live, streaming, state, run, stop, reset }
}

const profileStatusLabels: Record<AssistantProfileStatus, string> = {
  missing: "İndirilmedi",
  downloading: "İndiriliyor",
  downloaded: "İndirildi",
  installing: "Kuruluyor",
  ready: "Kullanıma hazır",
  error: "Kurulum başarısız",
}

const runtimeStatusLabels: Record<AssistantRuntimeStatus, string> = {
  missing: "Kurulu değil",
  downloading: "İndiriliyor",
  downloaded: "İndirildi",
  installing: "Kuruluyor",
  ready: "Hazır",
  error: "Kurulum başarısız",
  unavailable: "Bu cihazda desteklenmiyor",
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

const numberFormatter = new Intl.NumberFormat("tr-TR", { maximumFractionDigits: 1 })

export function ProfileCard({ profile, manager, isActive, isRecommended }: {
  profile: AssistantProfile
  manager: AssistantManager
  isActive: boolean
  isRecommended: boolean
}) {
  const [confirming, setConfirming] = useState(false)
  const download = profile.download
  const active = downloadActive(download)
  const pendingKey = manager.pending?.key ?? ""
  const busy = pendingKey.startsWith(`${profile.id}:`)
  const error = manager.actionError?.key.startsWith(`${profile.id}:`) ? manager.actionError.message : null
  const percentage =
    download?.percentage === null || download?.percentage === undefined
      ? null
      : Math.max(0, Math.min(100, download.percentage))
  const autoInstalling =
    !!download && download.status === "completed" &&
    (profile.status === "installing" || pendingKey === `${profile.id}:install`)
  const showFreshDownload =
    profile.status === "missing" ||
    download?.status === "cancelled" ||
    (profile.status === "error" && download?.status !== "completed")

  return (
    <li className="space-y-4 py-6" aria-busy={busy}>
      <div className="flex flex-wrap items-center gap-3">
        <h3 className="min-w-0 break-words font-medium text-parchment">{profile.title}</h3>
        <span className={`rounded px-2 py-1 font-mono text-xs ${profile.status === "ready" ? "status-ok" : profile.status === "error" ? "status-bad" : "bg-concrete text-parchment"}`}>
          {profileStatusLabels[profile.status]}
        </span>
        {isActive && (
          <span className="rounded border border-olive/50 bg-olive/20 px-2 py-1 font-mono text-xs text-parchment">Etkin</span>
        )}
        {isRecommended && !isActive && (
          <span className="rounded border border-sand/30 bg-coal/60 px-2 py-1 font-mono text-xs text-sand/90">Önerilen</span>
        )}
      </div>
      <p className="break-words font-mono text-xs tabular-nums text-sand/80">
        {formatBytes(profile.size_bytes)} · {profile.model_name} · {profile.quantization} · Bağlam {profile.context_size}
      </p>
      <p className="break-words text-sm leading-6 text-sand/90">
        Yaklaşık {numberFormatter.format(profile.ram_gb)} GB RAM hedefi. Hedef değerdir, garanti değildir.
      </p>
      <p className="break-words font-mono text-xs leading-5 text-sand/70">
        Lisans: {profile.license} · Kaynak: {profile.source}
      </p>

      {profile.status === "error" && profile.error && (
        <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{profile.error.message}</p>
      )}

      {download && profile.status !== "ready" && (
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
                aria-label={`${profile.title} indirme ilerlemesi`}
              />
              <p className="flex flex-wrap justify-between gap-2 font-mono text-xs tabular-nums text-sand/80">
                <span>{formatBytes(download.downloadedBytes)} / {download.totalBytes === null ? "Toplam boyut bilinmiyor" : formatBytes(download.totalBytes)}</span>
                {percentage !== null && <span>%{numberFormatter.format(percentage)}</span>}
              </p>
            </>
          )}
          {download.error && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{download.error.message}</p>}
        </div>
      )}

      {error && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{error}</p>}

      <div className="flex flex-wrap gap-3">
        {showFreshDownload && (
          <button type="button" className={buttonClass} disabled={busy || !!manager.pending} onClick={() => void manager.startDownload(profile)}>
            {busy ? "Başlatılıyor…" : profile.status === "error" ? "Tekrar dene" : "İndir"}
          </button>
        )}
        {download && downloadControlActions(download.status).map((action) => (
          <button
            key={action}
            type="button"
            className={action === "cancel" ? secondaryButtonClass : buttonClass}
            disabled={busy || !!manager.pending && !busy}
            onClick={() => void manager.controlDownload(profile.id, download, action)}
          >
            {pendingKey.endsWith(`:${action}`) ? "İşleniyor…" : controlActionLabels[action]}
          </button>
        ))}
        {download?.status === "completed" && profile.status !== "ready" && !autoInstalling && (
          <button type="button" className={buttonClass} disabled={busy || !!manager.pending} onClick={() => void manager.installProfile(profile, download.id)}>
            {pendingKey.endsWith(":install") ? "Başlatılıyor…" : "Kur"}
          </button>
        )}
        {autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">İndirme tamamlandı. Model kuruluyor…</p>
        )}
        {profile.status === "installing" && !autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">Model kuruluyor… Bu işlem bitene kadar bekleyin.</p>
        )}
        {profile.status === "ready" && !isActive && (
          <button type="button" className={buttonClass} disabled={busy || !!manager.pending} onClick={() => void manager.activateProfile(profile)}>
            {pendingKey.endsWith(":activate") ? "Etkinleştiriliyor…" : "Etkinleştir"}
          </button>
        )}
        {profile.status === "ready" && isActive && (
          <p role="status" className="font-mono text-xs leading-6 text-olive">Bu profil sohbet için kullanılıyor.</p>
        )}
        {(profile.status === "ready" || profile.status === "downloaded" || profile.status === "error") && (
          confirming ? (
            <span className="flex flex-wrap items-center gap-3">
              <span className="text-sm text-parchment">Kalıcı olarak silinsin mi?</span>
              <button type="button" className={buttonClass} disabled={busy} onClick={() => { setConfirming(false); void manager.removeProfile(profile) }}>
                Evet, sil
              </button>
              <button type="button" className={secondaryButtonClass} disabled={busy} onClick={() => setConfirming(false)}>
                Vazgeç
              </button>
            </span>
          ) : (
            <button type="button" className={secondaryButtonClass} disabled={busy || !!manager.pending} onClick={() => setConfirming(true)}>
              Sil
            </button>
          )
        )}
      </div>
    </li>
  )
}

export function RuntimePanel({ manager }: { manager: AssistantManager }) {
  const snapshot = manager.snapshot
  const runtime = snapshot?.runtime ?? null
  const download = runtime?.download ?? null
  const pendingKey = manager.pending?.key ?? ""
  const busy = pendingKey.startsWith("runtime:")
  const error = manager.actionError?.key.startsWith("runtime:") ? manager.actionError.message : null
  const percentage =
    download?.percentage === null || download?.percentage === undefined
      ? null
      : Math.max(0, Math.min(100, download.percentage))
  const autoInstalling =
    !!download && download.status === "completed" &&
    (runtime?.status === "installing" || pendingKey === "runtime:install")
  const showFreshDownload =
    !!runtime &&
    runtime.status !== "ready" &&
    runtime.status !== "installing" &&
    runtime.status !== "unavailable" &&
    runtime.status !== "downloading" &&
    (!download ||
      download.status === "cancelled" ||
      download.status === "failed" ||
      runtime.status === "missing" ||
      (runtime.status === "error" && download.status !== "completed"))

  return (
    <div className="mt-6 space-y-3 rounded-md border border-sand/20 bg-coal/50 px-4 py-5" aria-busy={busy}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-mono text-xs uppercase tracking-wider text-parchment">Çalışma motoru (CPU)</h3>
        {runtime && (
          <span className={`rounded px-2 py-1 font-mono text-xs ${runtime.status === "ready" ? "status-ok" : runtime.status === "error" ? "status-bad" : "bg-concrete text-parchment"}`}>
            {runtimeStatusLabels[runtime.status]}
          </span>
        )}
      </div>
      <p className="max-w-prose text-sm leading-6 text-sand/90">
        Sohbet için gerekli yerel motor. Cihazda çalışır, internete bağlanmaz.
        {runtime?.version ? ` Sürüm ${runtime.version}.` : ""}
      </p>
      {runtime?.status === "unavailable" && (
        <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">
          Bu cihazda motor desteklenmiyor. {runtime.error?.message ?? "Kurulum yapılamıyor."}
        </p>
      )}
      {runtime?.status === "error" && runtime.error && (
        <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{runtime.error.message}</p>
      )}
      {download && runtime && runtime.status !== "ready" && runtime.status !== "unavailable" && (
        <div className="space-y-2">
          <p role="status" className="inline-flex items-center gap-2 rounded border border-sand/20 bg-coal/60 px-2 py-1 font-mono text-xs uppercase tracking-wider text-parchment">
            {downloadStatusLabels[download.status]}
          </p>
          {(downloadActive(download) || download.status === "paused") && (
            <>
              <progress className="download-progress" max={100} value={percentage ?? undefined} aria-label="Çalışma motoru indirme ilerlemesi" />
              <p className="flex flex-wrap justify-between gap-2 font-mono text-xs tabular-nums text-sand/80">
                <span>{formatBytes(download.downloadedBytes)} / {download.totalBytes === null ? "Toplam boyut bilinmiyor" : formatBytes(download.totalBytes)}</span>
                {percentage !== null && <span>%{numberFormatter.format(percentage)}</span>}
              </p>
            </>
          )}
          {download.error && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{download.error.message}</p>}
        </div>
      )}
      {error && <p role="alert" className="max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">{error}</p>}
      <div className="flex flex-wrap gap-3">
        {showFreshDownload && runtime?.status !== "unavailable" && (
          <button type="button" className={buttonClass} disabled={busy || !!manager.pending} onClick={() => void manager.startRuntimeDownload()}>
            {busy ? "Başlatılıyor…" : runtime?.status === "error" || download?.status === "cancelled" || download?.status === "failed" ? "Tekrar dene" : "Motoru indir"}
          </button>
        )}
        {download && downloadControlActions(download.status).map((action) => (
          <button
            key={action}
            type="button"
            className={action === "cancel" ? secondaryButtonClass : buttonClass}
            disabled={busy || !!manager.pending && !busy}
            onClick={() => void manager.controlDownload("runtime", download, action)}
          >
            {pendingKey.endsWith(`:${action}`) ? "İşleniyor…" : controlActionLabels[action]}
          </button>
        ))}
        {download?.status === "completed" && runtime && runtime.status !== "ready" && runtime.status !== "unavailable" && !autoInstalling && (
          <button type="button" className={buttonClass} disabled={busy || !!manager.pending} onClick={() => void manager.installRuntime(download.id)}>
            {pendingKey.endsWith(":install") ? "Başlatılıyor…" : "Motoru kur"}
          </button>
        )}
        {autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">İndirme tamamlandı. Motor kuruluyor…</p>
        )}
        {runtime?.status === "installing" && !autoInstalling && (
          <p role="status" className="font-mono text-xs leading-6 text-sand/80">Motor kuruluyor… Bu işlem bitene kadar bekleyin.</p>
        )}
        {runtime?.status === "ready" && (
          <p role="status" className="font-mono text-xs leading-6 text-olive">Motor hazır.</p>
        )}
      </div>
    </div>
  )
}

export function SourceList({ sources }: { sources: ChatSource[] }) {
  if (sources.length === 0) return null
  return (
    <div className="space-y-2">
      <p className="font-mono text-xs uppercase tracking-wider text-sand/70">Yerel kaynaklar</p>
      <ul className="space-y-3">
        {sources.map((source) => (
          <li key={source.id} className="rounded border border-sand/15 bg-coal/50 px-3 py-2">
            <a
              href={source.href}
              className="break-words underline decoration-sand/40 underline-offset-4 hover:decoration-sand focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal"
            >
              {source.title}
            </a>
            {source.excerpt && (
              <blockquote className="mt-2 max-w-prose break-words border-l-2 border-sand/30 pl-3 text-sm leading-6 text-sand/90">
                <span className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Doğrudan alıntı: </span>
                {source.excerpt}
              </blockquote>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}

export function AssistantView() {
  const manager = useAssistantStatus()
  const chat = useAssistantChat()
  const [prompt, setPrompt] = useState("")
  const [selectedId, setSelectedId] = useState("")
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const answerRef = useRef<HTMLDivElement>(null)

  const snapshot = manager.snapshot
  const profiles = snapshot ? orderedProfiles(snapshot.profiles) : []
  const recommended = snapshot?.recommended_profile_id ?? ""
  const effectiveId = selectedId || snapshot?.active_profile_id || recommended || profiles[0]?.id || ""
  const selected = profiles.find((profile) => profile.id === effectiveId) ?? activeProfile(snapshot) ?? profiles.find((profile) => profile.status === "ready")
  const turns = chat.turns
  const streaming = chat.streaming
  const length = countCharacters(prompt)
  const overLimit = length > MAX_PROMPT_CHARS
  const canSend = !!snapshot && canChatNow(snapshot, selected, prompt) && !streaming
  const hardware = snapshot?.hardware ?? null
  const hasReadyModel = profiles.some((profile) => profile.status === "ready")
  const lastTurn = turns.length > 0 ? turns[turns.length - 1] : null

  useEffect(() => {
    if (!selectedId && snapshot?.active_profile_id) setSelectedId(snapshot.active_profile_id)
  }, [snapshot?.active_profile_id, selectedId])

  function selectProfile(id: string) {
    if (streaming) chat.stop(true)
    setSelectedId(id)
  }

  function clearInput() {
    setPrompt("")
    inputRef.current?.focus()
  }

  function clearTranscript() {
    if (streaming) chat.stop(false)
    else chat.reset()
    inputRef.current?.focus()
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || streaming) return
    const current = prompt
    setPrompt("")
    void chat.run(current, selected.id)
  }

  function shortcut(event: KeyboardEvent<HTMLTextAreaElement>) {
    if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
      event.preventDefault()
      if (canSend && selected) {
        const current = prompt
        setPrompt("")
        void chat.run(current, selected.id)
      }
    }
  }

  useEffect(() => {
    if (lastTurn && lastTurn.status !== "streaming") answerRef.current?.focus()
  }, [lastTurn?.id, lastTurn?.status])

  const selectedRamBytes = (selected?.ram_gb ?? 0) * 1000000000
  const availableBytes = hardware?.available_memory_bytes ?? null
  const totalBytes = hardware?.total_memory_bytes ?? null
  const ramShortage =
    selected && availableBytes !== null && selectedRamBytes > 0 && selectedRamBytes > availableBytes
      ? selectedRamBytes > (totalBytes ?? Number.POSITIVE_INFINITY)
        ? "critical"
        : "low"
      : null

  return (
    <section aria-labelledby="assistant-heading">
      <p className="terminal-label">08 / Asistan</p>
      <h2 id="assistant-heading" className="mt-2 font-display text-4xl tracking-wide text-parchment">ASİSTAN</h2>
      <p className="mt-2 max-w-prose text-sm leading-6 text-sand/90">
        İsteğe bağlı asistan, cihazınızda çalışır. Sohbetler diske kaydedilmez. Başlamak için motor ve bir model kurun.
      </p>
      <p role="note" className="mt-4 max-w-prose rounded border border-sand/20 bg-coal/50 px-3 py-2 text-sm leading-6 text-sand/90">
        Eğitim amaçlıdır. Tıbbi öneri yerine geçmez; acil durumda resmi kaynaklara başvurun.
      </p>

      {manager.state.status === "loading" && (
        <p role="status" className="mt-8 font-mono text-sm text-sand/80">Asistan durumu yükleniyor…</p>
      )}
      {manager.state.status === "error" && (
        <div className="mt-8 rounded-md border border-blood/60 bg-blood/10 px-4 py-6">
          <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">{manager.state.message}</p>
          <button type="button" className={`${secondaryButtonClass} mt-4`} onClick={manager.reload}>Tekrar dene</button>
        </div>
      )}

      {snapshot && (
        <>
          {!snapshot.runtime.available && (
            <div className="mt-6 rounded-md border border-blood/60 bg-blood/10 px-4 py-5">
              <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">
                Çalışma motoru bu kurulumda hazır değil. Önce motoru indirip kurun, ardından model seçin.
              </p>
            </div>
          )}
          {snapshot.busy && (
            <p role="status" className="mt-4 max-w-prose rounded border border-sand/20 bg-coal/50 px-3 py-2 text-sm leading-6 text-sand/90">
              Başka bir işlem sürüyor. Bitene kadar bekleyin.
            </p>
          )}
          {hardware && (
            <div className="mt-4 space-y-1">
              <p className="font-mono text-xs tabular-nums text-sand/70">
                Cihaz: {hardware.cpu_count} CPU · Kullanılabilir {formatBytes(hardware.available_memory_bytes)} / Toplam {formatBytes(hardware.total_memory_bytes)}
              </p>
              {selected && (
                <p className="font-mono text-xs tabular-nums text-sand/70">
                  Seçili model hedefi: yaklaşık {numberFormatter.format(selected.ram_gb)} GB RAM. Hedef değerdir, garanti değildir.
                </p>
              )}
            </div>
          )}
          {ramShortage && selected && availableBytes !== null && (
            <p role={ramShortage === "critical" ? "alert" : "status"} className={`mt-4 max-w-prose rounded border px-3 py-2 text-sm leading-6 ${ramShortage === "critical" ? "border-blood/60 bg-blood/10 text-parchment" : "border-sand/20 bg-coal/50 text-sand/90"}`}>
              {ramShortage === "critical"
                ? `Seçili model yaklaşık ${numberFormatter.format(selected.ram_gb)} GB RAM hedefler; cihaz toplamı ${formatBytes(totalBytes ?? 0)} ile hedefin altında. Başlamayabilir ya da yarıda kesilebilir.`
                : `Seçili model yaklaşık ${numberFormatter.format(selected.ram_gb)} GB RAM hedefler; kullanılabilir ${formatBytes(availableBytes)} ile hedefin altında. Yanıt yavaşlayabilir ya da kesilebilir.`}
            </p>
          )}

          <details open={!hasReadyModel} className="mt-10 rounded-md border border-sand/20 bg-coal/50 px-4 py-3">
            <summary className="cursor-pointer text-sm font-medium text-parchment focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand focus-visible:ring-offset-2 focus-visible:ring-offset-coal">
              Model ve motor yönetimi
            </summary>
            <div className="mt-4">
              <RuntimePanel manager={manager} />

              <div className="mt-8">
                <h3 className="font-mono text-xs uppercase tracking-wider text-sand/70">Modeller</h3>
                <p className="mt-1 max-w-prose text-sm leading-6 text-sand/80">
                  Bir model seçin. Boyut ve dağıtıcı bilgilerini inceleyin. İndirme yalnızca siz başlatırsanız yapılır.
                </p>
                <ul className="mt-2 divide-y divide-sand/15 border-y border-sand/15">
                  {profiles.map((profile) => (
                    <ProfileCard
                      key={profile.id}
                      profile={profile}
                      manager={manager}
                      isActive={snapshot.active_profile_id === profile.id && profile.status === "ready"}
                      isRecommended={recommended === profile.id}
                    />
                  ))}
                </ul>
              </div>
            </div>
          </details>

          <div className="mt-10">
            <h3 className="font-mono text-xs uppercase tracking-wider text-sand/70">Sohbet</h3>
            <p className="mt-1 max-w-prose text-sm leading-6 text-sand/80">
              Her soru bağımsız yanıtlanır. Sohbet geçmişi yalnızca bu ekranda tutulur, kapatınca silinir.
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-3">
              <label htmlFor="assistant-profile" className="text-sm font-medium text-parchment">Kullanılacak model</label>
              <select
                id="assistant-profile"
                value={selected?.id ?? ""}
                onChange={(event) => selectProfile(event.target.value)}
                className="field-terminal min-w-0 max-w-full sm:max-w-xs"
              >
                {profiles.map((profile) => (
                  <option key={profile.id} value={profile.id}>
                    {profile.title}{profile.status !== "ready" ? " (hazır değil)" : ""}{snapshot.active_profile_id === profile.id ? " (etkin)" : ""}
                  </option>
                ))}
              </select>
            </div>
            {selected && (
              <p role="status" className="mt-2 font-mono text-xs text-sand/70">
                Sohbette kullanılacak model: {selected.title}{snapshot.active_profile_id === selected.id ? " (varsayılan)" : ""}
              </p>
            )}

            <form onSubmit={submit} className="mt-4 space-y-5" aria-label="Yerel asistana sor">
              <div>
                <label htmlFor="assistant-input" className="mb-2 block text-sm font-medium text-parchment">Sorunuz</label>
                <textarea
                  id="assistant-input"
                  ref={inputRef}
                  rows={5}
                  value={prompt}
                  onChange={(event) => setPrompt(event.target.value)}
                  onKeyDown={shortcut}
                  aria-describedby="assistant-help assistant-count"
                  placeholder="Yerel arşive dayalı sorunuzu yazın"
                  className="field-terminal min-h-32 resize-y leading-7"
                />
                <div className="mt-2 flex flex-wrap items-baseline justify-between gap-2">
                  <p id="assistant-help" className="text-sm leading-6 text-sand/70">
                    {!snapshot.runtime.available
                      ? "Motor hazır olmadığı için sohbet yapılamıyor."
                      : !selected || selected.status !== "ready"
                        ? "Bu modeli kullanmak için önce indirip kurun ve etkinleştirin."
                        : "Göndermek için Ctrl+Enter kullanın veya Sor düğmesini seçin."}
                  </p>
                  <p id="assistant-count" role="status" aria-atomic="true" className={`font-mono text-xs tabular-nums ${overLimit ? "font-semibold text-parchment" : "text-sand/70"}`}>
                    {new Intl.NumberFormat("tr-TR").format(length)} / {new Intl.NumberFormat("tr-TR").format(MAX_PROMPT_CHARS)} karakter
                  </p>
                </div>
                {overLimit && (
                  <p role="alert" className="mt-2 max-w-prose rounded border border-blood/60 bg-blood/10 px-3 py-2 text-sm leading-6 text-parchment">
                    Soru en fazla {new Intl.NumberFormat("tr-TR").format(MAX_PROMPT_CHARS)} karakter olabilir. Kısaltıp tekrar deneyin.
                  </p>
                )}
                <div className="mt-4 flex flex-wrap gap-3">
                  <button type="submit" className={buttonClass} disabled={!canSend}>
                    {streaming ? "Yanıtlanıyor…" : "Sor"}
                  </button>
                  {streaming && (
                    <button type="button" className={secondaryButtonClass} onClick={() => chat.stop(true)}>
                      Durdur
                    </button>
                  )}
                  <button type="button" className={secondaryButtonClass} disabled={!prompt} onClick={clearInput}>
                    Temizle
                  </button>
                  {turns.length > 0 && !streaming && (
                    <button type="button" className={secondaryButtonClass} onClick={clearTranscript}>
                      Sohbeti temizle
                    </button>
                  )}
                </div>
              </div>
            </form>

            <div className="mt-8" aria-busy={streaming}>
              <h3 className="text-sm font-medium text-parchment">Sohbet geçmişi</h3>
              <div ref={answerRef} tabIndex={-1} aria-live="polite" aria-atomic="true" className="mt-2 min-h-24 rounded-md border border-sand/20 bg-coal/60 px-4 py-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sand">
                {turns.length === 0 && (
                  <p className="text-sm leading-6 text-sand/70">Sorunuz ve yanıt burada görünecek.</p>
                )}
                {turns.length > 0 && (
                  <ul className="space-y-4">
                    {turns.map((turn) => {
                      const turnProfile = profiles.find((profile) => profile.id === turn.profileId)
                      const turnProfileTitle = turnProfile?.title ?? turn.profileId
                      const retryable =
                        turn.status === "error" &&
                        turn.prompt.trim().length > 0 &&
                        countCharacters(turn.prompt) <= MAX_PROMPT_CHARS
                      return (
                        <li key={turn.id} className="space-y-3 rounded border border-sand/15 bg-coal/50 px-3 py-3">
                          <p className="break-words text-sm leading-6 text-parchment">
                            <span className="font-mono text-[11px] uppercase tracking-wider text-sand/70">Siz ({turnProfileTitle}): </span>
                            {turn.prompt}
                          </p>
                          {turn.status === "streaming" && (
                            <div className="space-y-3">
                              <p role="status" className="font-mono text-xs uppercase tracking-wider text-sand/80">
                                {turn.phase === "searching" ? "Yerel kaynaklar aranıyor…" : turn.phase === "loading" ? "Model hazırlanıyor…" : "Yanıt üretiliyor…"}
                              </p>
                              {turn.text ? (
                                <p className="max-w-prose whitespace-pre-wrap break-words text-base leading-7 text-parchment">{turn.text}</p>
                              ) : (
                                <p className="font-mono text-sm text-sand/80">Hazırlanıyor…</p>
                              )}
                              <SourceList sources={turn.sources} />
                            </div>
                          )}
                          {turn.status === "ready" && (
                            <div className="space-y-3">
                              {turn.interrupted && (
                                <p role="status" className="font-mono text-xs uppercase tracking-wider text-sand/80">Durduruldu, eksik yanıt.</p>
                              )}
                              <p className="font-mono text-xs uppercase tracking-wider text-sand/70">Üretilen yanıt</p>
                              <p className="max-w-prose whitespace-pre-wrap break-words text-base leading-7 text-parchment">{turn.text}</p>
                              {turn.sources.length > 0 ? (
                                <SourceList sources={turn.sources} />
                              ) : (
                                <p role="status" className="max-w-prose text-sm leading-6 text-sand/80">
                                  Bu soru için yeterli yerel kaynak bulunamadı. İlgili bir bilgi paketi kurup tekrar deneyin.
                                </p>
                              )}
                              {!turn.grounded && turn.sources.length === 0 && (
                                <p role="status" className="max-w-prose text-sm leading-6 text-sand/80">
                                  Kaynaksız yanıt. İlgili paketi kurup tekrar sorun.
                                </p>
                              )}
                            </div>
                          )}
                          {turn.status === "error" && (
                            <div className="space-y-3">
                              <p role="alert" className="max-w-prose text-sm leading-6 text-parchment">{turn.message}</p>
                              {retryable && (
                                <button
                                  type="button"
                                  className={`${secondaryButtonClass}`}
                                  disabled={streaming || !turnProfile || turnProfile.status !== "ready"}
                                  onClick={() => void chat.run(turn.prompt, turn.profileId)}
                                >
                                  Tekrar dene
                                </button>
                              )}
                            </div>
                          )}
                        </li>
                      )
                    })}
                  </ul>
                )}
              </div>
              <p className="mt-3 max-w-prose text-xs leading-5 text-sand/70">
                Yanıtlar cihazda üretilir. Sohbet bu ekrandan ayrılınca silinir. Kaynak bağlantıları kurulu yerel içeriği açar.
              </p>
            </div>
          </div>
        </>
      )}
    </section>
  )
}
