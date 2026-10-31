import { useCallback, useEffect, useRef, useState } from "react"
import { buttonClass, downloadRequest, secondaryButtonClass } from "./downloads"

export type RuntimeResponse = {
  runtime: {
    state: "stopped" | "starting" | "ready" | "stopping" | "failed"
    error: { code: "runtime_unavailable"; message: string } | null
  }
  contents: { packageId: string; href: string }[]
}

function validResponse(value: RuntimeResponse): boolean {
  if (!value?.runtime || !["stopped", "starting", "ready", "stopping", "failed"].includes(value.runtime.state)) return false
  if (!Array.isArray(value.contents)) return false
  if (value.runtime.state === "failed") {
    if (value.runtime.error?.code !== "runtime_unavailable" || typeof value.runtime.error.message !== "string") return false
  } else if (value.runtime.error !== null) return false
  if (value.runtime.state !== "ready" && value.contents.length > 0) return false
  return value.contents.every((item) => /^[a-f0-9]{32}$/.test(item?.packageId) &&
    /^\/content\/content\/[a-f0-9]{32}\/$/.test(item?.href) && item.href.split("/")[3] === item.packageId)
}

export function useRuntime() {
  const [snapshot, setSnapshot] = useState<RuntimeResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [retrying, setRetrying] = useState(false)
  const actions = useRef({ retry: () => {}, reload: () => {} })

  useEffect(() => {
    let disposed = false
    let request: { controller: AbortController; retry: boolean } | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let next: "load" | "retry" | null = null
    const hidden = () => document.visibilityState === "hidden"

    async function load(retry = false) {
      if (disposed || hidden() || request) return
      clearTimeout(timer)
      const active = { controller: new AbortController(), retry }
      request = active
      if (retry) {
        setRetrying(true)
        setError(null)
      }
      try {
        const result = await downloadRequest<RuntimeResponse>(retry ? "/api/runtime/retry" : "/api/runtime", active.controller.signal, retry ? {} : undefined)
        if (!validResponse(result)) throw new Error("İçerik hizmetinden geçerli bir yanıt alınamadı. Tekrar deneyin.")
        if (!disposed && !active.controller.signal.aborted) {
          setSnapshot(result)
          setError(null)
        }
      } catch (failure) {
        if (!disposed && !active.controller.signal.aborted) {
          setError(failure instanceof Error ? failure.message : "İçerik hizmetine ulaşılamadı. Tekrar deneyin.")
        }
      } finally {
        if (request === active) request = null
        if (!disposed) {
          if (retry) setRetrying(false)
          if (!active.controller.signal.aborted) setLoading(false)
          if (!hidden()) {
            const queued = next
            next = null
            timer = setTimeout(() => void load(queued === "retry"), queued ? 0 : 2000)
          }
        }
      }
    }

    function schedule(retry: boolean) {
      if (disposed || hidden() || (request?.retry && !request.controller.signal.aborted) || next === "retry") return
      clearTimeout(timer)
      if (request) {
        next = retry ? "retry" : "load"
        request.controller.abort()
      } else {
        void load(retry)
      }
    }

    function visibilityChanged() {
      clearTimeout(timer)
      next = null
      if (hidden()) {
        request?.controller.abort()
      } else {
        schedule(false)
      }
    }

    actions.current = { retry: () => schedule(true), reload: () => schedule(false) }
    document.addEventListener("visibilitychange", visibilityChanged)
    void load()
    return () => {
      disposed = true
      clearTimeout(timer)
      request?.controller.abort()
      actions.current = { retry: () => {}, reload: () => {} }
      document.removeEventListener("visibilitychange", visibilityChanged)
    }
  }, [])

  const retry = useCallback(() => actions.current.retry(), [])
  const reload = useCallback(() => actions.current.reload(), [])
  return { snapshot, loading, error, retrying, retry, reload }
}

type RuntimeManager = ReturnType<typeof useRuntime>

export function RuntimeStatus({ manager, installedCount }: { manager: RuntimeManager; installedCount: number | null }) {
  const state = manager.snapshot?.runtime.state
  const busy = manager.retrying || state === "starting" || state === "stopping"
  const status = manager.retrying ? "İçerik hizmeti yeniden başlatılıyor…"
    : state === "starting" ? "İçerik hizmeti başlatılıyor… Kurulu paketleriniz hazırlanıyor."
    : state === "stopping" ? "İçerik hizmeti yenileniyor…"
    : state === "ready" ? "İçerik hizmeti hazır. Kullanıma hazır paketleri çevrimdışı açabilirsiniz."
    : state === "stopped" ? installedCount === null ? "Kurulu içerikler kontrol ediliyor…" : installedCount > 0 ? "Kurulu içerikler henüz kullanıma hazır değil." : "Henüz kurulu içerik yok. Doğrulanıp kurulan paketler burada açılabilir."
    : null

  return (
    <div className="mt-6 space-y-3 border-y py-5" aria-busy={busy}>
      {manager.loading && !manager.error && <p role="status" className="text-sm text-muted-foreground">İçerik hizmetinin durumu kontrol ediliyor…</p>}
      {!manager.error && status && <p role="status" className="text-sm leading-6 text-muted-foreground">{status}</p>}
      {!manager.error && state === "failed" && <p role="alert" className="text-sm leading-6">{manager.snapshot?.runtime.error?.message}</p>}
      {manager.error && (
        <div className="space-y-3">
          <p role="alert" className="max-w-prose text-sm leading-6">İçerik durumu yenilenemedi. {manager.error}</p>
          <button type="button" className={secondaryButtonClass} disabled={busy} onClick={manager.reload}>İçerik durumunu yenile</button>
        </div>
      )}
      {(installedCount ?? 0) > 0 && (state === "failed" || state === "stopped" || busy) && (
        <button type="button" className={buttonClass} disabled={busy} onClick={manager.retry}>
          {manager.retrying ? "Yeniden başlatılıyor…" : "İçerik hizmetini tekrar dene"}
        </button>
      )}
    </div>
  )
}

export function InstalledContent({ packageId, title, variant, manager }: { packageId: string; title: string; variant: string; manager: RuntimeManager }) {
  const content = !manager.error && !manager.retrying && manager.snapshot?.runtime.state === "ready"
    ? manager.snapshot.contents.find((item) => item.packageId === packageId)
    : undefined
  return content ? (
    <a href={content.href} className={`${buttonClass} inline-flex items-center`} aria-label={`${title} ${variant}: İçeriği aç`}>İçeriği aç</a>
  ) : (
    <p role="status" className="max-w-prose text-sm leading-6 text-muted-foreground">
      {manager.error ? "Paket kurulu. İçerik durumu kontrol edilemiyor."
        : manager.snapshot?.runtime.state === "failed" ? "Paket kurulu. İçeriği açmak için içerik hizmetini tekrar deneyin."
        : "Paket kurulu. İçeriğin kullanıma hazır olması bekleniyor…"}
    </p>
  )
}
