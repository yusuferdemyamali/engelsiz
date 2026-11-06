import assert from "node:assert/strict"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { readFileSync } from "node:fs"
import { test } from "node:test"
import { build } from "esbuild"

const require = createRequire(import.meta.url)
const React = require("react")
const { renderToStaticMarkup } = require("react-dom/server")
const compiled = await build({
  entryPoints: [fileURLToPath(new URL("../src/assistant.tsx", import.meta.url))],
  bundle: true,
  platform: "node",
  format: "cjs",
  external: ["react", "react/jsx-runtime"],
  write: false,
})
const module = { exports: {} }
new Function("require", "module", "exports", compiled.outputFiles[0].text)(require, module, module.exports)
const {
  countCharacters,
  validatePromptInput,
  MAX_PROMPT_CHARS,
  isLocalSourceHref,
  validAssistantStatus,
  downloadControlActions,
  extractDownloadPayload,
  extractProfilePayload,
  shouldAutoInstallProfile,
  shouldAutoInstallRuntime,
  activeProfile,
  canChatNow,
  orderedProfiles,
  parseSseBlock,
  useAssistantChat,
  SourceList,
  AssistantView,
} = module.exports

// Faithful minimal dispatcher: stable useCallback, dependency-aware useEffect
// with cleanup. Required so rerender-cancellation regressions (e.g. an
// erroneous [chat] dep around chat.stop) are caught instead of masked.
function mountHookFor(hookFn) {
  const slots = []
  const memos = []
  const effects = []
  let index = 0
  function depsEqual(a, b) {
    if (a === undefined || b === undefined) return false
    if (a.length !== b.length) return false
    return a.every((value, i) => Object.is(value, b[i]))
  }
  const dispatcher = {
    useState(initial) {
      const slot = index++
      if (!(slot in slots)) slots[slot] = typeof initial === "function" ? initial() : initial
      const set = (value) => { slots[slot] = typeof value === "function" ? value(slots[slot]) : value }
      return [slots[slot], set]
    },
    useRef(initial) {
      const slot = index++
      return (slots[slot] ??= { current: initial })
    },
    useCallback(callback, deps) {
      const slot = index++
      const prev = memos[slot]
      if (!prev || !depsEqual(prev.deps, deps)) memos[slot] = { deps, callback }
      return memos[slot].callback
    },
    useMemo(factory, deps) {
      const slot = index++
      const prev = memos[`memo:${slot}`]
      if (!prev || !depsEqual(prev.deps, deps)) memos[`memo:${slot}`] = { deps, value: factory() }
      return memos[`memo:${slot}`].value
    },
    useEffect(callback, deps) {
      const slot = index++
      const prev = effects[slot]
      if (!prev) {
        effects[slot] = { deps, cleanup: callback() }
      } else if (!depsEqual(prev.deps, deps)) {
        try { prev.cleanup?.() } catch { /* yoksay */ }
        effects[slot] = { deps, cleanup: callback() }
      }
    },
  }
  return {
    read() {
      index = 0
      const internal = React.__SECRET_INTERNALS_DO_NOT_USE_OR_YOU_WILL_BE_FIRED.ReactCurrentDispatcher
      const previous = internal.current
      internal.current = dispatcher
      try { return hookFn() } finally { internal.current = previous }
    },
    unmount() {
      for (const entry of effects) {
        try { entry?.cleanup?.() } catch { /* yoksay */ }
      }
    },
  }
}

function mountChat() {
  return mountHookFor(useAssistantChat)
}

function flush(times = 5) {
  const step = () => new Promise((resolve) => setImmediate(resolve))
  return (async () => { for (let i = 0; i < times; i++) await step() })()
}

const enc = new TextEncoder()

function sseReader(chunks) {
  const queue = [...chunks]
  let cancelled = false
  return {
    cancelled: () => cancelled,
    reader: {
      read: async () => {
        if (cancelled) return { done: true, value: undefined }
        if (queue.length === 0) return { done: true, value: undefined }
        return { done: false, value: queue.shift() }
      },
      cancel: async () => { cancelled = true },
    },
  }
}

function chatResponse(chunks, { ok = true, contentType = "text/event-stream" } = {}) {
  const { reader } = sseReader(chunks)
  if (!ok) {
    return { ok: false, headers: { get: () => contentType }, json: async () => ({ error: { message: "Yanıt alınamadı. Tekrar deneyin." } }), body: null }
  }
  return { ok: true, headers: { get: (name) => (name.toLowerCase() === "content-type" ? contentType : null) }, json: async () => null, body: { getReader: () => reader } }
}

function stubFetch(handler) {
  const original = globalThis.fetch
  globalThis.fetch = handler
  return () => { globalThis.fetch = original }
}

function sseEvent(event, data) {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
}

// Controllable stream: push() deltas, finish() ends the body.
function controllableSse() {
  const queue = []
  const waiters = []
  let cancelled = false
  let finished = false
  const reader = {
    read: async () => {
      for (;;) {
        if (cancelled) return { done: true, value: undefined }
        if (queue.length > 0) return { done: false, value: queue.shift() }
        if (finished) return { done: true, value: undefined }
        await new Promise((resolve) => waiters.push(resolve))
      }
    },
    cancel: async () => { cancelled = true; waiters.splice(0).forEach((r) => r()) },
  }
  return {
    reader,
    wasCancelled: () => cancelled,
    push: (text) => { queue.push(enc.encode(text)); waiters.splice(0).forEach((r) => r()) },
    finish: () => { finished = true; waiters.splice(0).forEach((r) => r()) },
  }
}

function downloadFixture(overrides = {}) {
  return {
    id: "dl-1",
    provider: "yerel",
    providerPackageId: "asistan-lite",
    title: "Asistan modeli",
    variant: "lite",
    fileName: "lite.bin",
    status: "queued",
    downloadedBytes: 0,
    totalBytes: 1000,
    percentage: 0,
    error: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  }
}

function profileFixture(overrides = {}) {
  return {
    id: "lite",
    title: "Hafif profil",
    model_name: "yerel-lite",
    quantization: "q4",
    size_bytes: 1000,
    ram_gb: 2,
    context_size: 2048,
    license: "Apache-2.0",
    source: "yerel",
    status: "ready",
    download: null,
    error: null,
    ...overrides,
  }
}

function statusFixture(overrides = {}) {
  return {
    profiles: [profileFixture()],
    active_profile_id: "lite",
    recommended_profile_id: "lite",
    hardware: { total_memory_bytes: 8000, available_memory_bytes: 4000, cpu_count: 4 },
    runtime: { available: true, status: "ready", download: null, error: null, version: "1.0" },
    busy: false,
    ...overrides,
  }
}

test("prompt bounds are unicode-aware and capped at 2000", () => {
  assert.equal(MAX_PROMPT_CHARS, 2000)
  assert.equal(countCharacters("Türkçe"), 6)
  assert.equal("😀".length, 2)
  assert.equal(countCharacters("😀"), 1)
  assert.match(validatePromptInput("   ", MAX_PROMPT_CHARS), /önce bir soru/)
  assert.match(validatePromptInput("x".repeat(2001), MAX_PROMPT_CHARS), /2000/)
  assert.match(validatePromptInput("😀".repeat(2001), MAX_PROMPT_CHARS), /2000/)
  assert.equal(validatePromptInput("😀".repeat(2000), MAX_PROMPT_CHARS), null)
  assert.equal(validatePromptInput("yerel arşivde ne var?", MAX_PROMPT_CHARS), null)
})

test("profile/status contract accepts shaped snapshots and gates chat", () => {
  assert.equal(validAssistantStatus(statusFixture()), true)
  assert.equal(validAssistantStatus(null), false)
  assert.equal(validAssistantStatus({ ...statusFixture(), profiles: [] }), false)
  assert.equal(validAssistantStatus({ ...statusFixture(), busy: "evet" }), false)
  assert.equal(activeProfile(statusFixture())?.id, "lite")
  assert.equal(activeProfile({ ...statusFixture(), profiles: [profileFixture({ status: "missing" })] }), undefined)
  const ordered = orderedProfiles([profileFixture({ id: "quality" }), profileFixture({ id: "lite" }), profileFixture({ id: "balanced" })])
  assert.deepEqual(ordered.map((p) => p.id), ["lite", "balanced", "quality"])
  const snapshot = statusFixture()
  assert.equal(canChatNow(snapshot, profileFixture(), "merhaba"), true)
  assert.equal(canChatNow({ ...snapshot, runtime: { ...snapshot.runtime, available: false } }, profileFixture(), "merhaba"), false)
  assert.equal(canChatNow({ ...snapshot, busy: true }, profileFixture(), "merhaba"), false)
  assert.equal(canChatNow(snapshot, profileFixture({ status: "missing" }), "merhaba"), false)
  assert.equal(canChatNow(snapshot, profileFixture(), "   "), false)
  assert.equal(canChatNow(snapshot, profileFixture(), "x".repeat(2001)), false)
  assert.equal(canChatNow(null, profileFixture(), "merhaba"), false)
})

test("source hrefs allow only local content paths", () => {
  assert.equal(isLocalSourceHref("/content/abc"), true)
  assert.equal(isLocalSourceHref("/education/content/abc"), true)
  assert.equal(isLocalSourceHref("https://ornek.test/content/abc"), false)
  assert.equal(isLocalSourceHref("http://engelsiz.invalid/content/abc"), false)
  assert.equal(isLocalSourceHref("javascript:alert(1)"), false)
  assert.equal(isLocalSourceHref("/etc/passwd"), false)
  assert.equal(isLocalSourceHref("/content/../etc"), false)
  assert.equal(isLocalSourceHref("/content/a?b=c"), false)
  assert.equal(isLocalSourceHref("/content/a#b"), false)
  assert.equal(isLocalSourceHref(""), false)
})

test("source hrefs reject dot-segment traversal out of local roots", () => {
  assert.equal(isLocalSourceHref("/content/../../api/assistant"), false)
  assert.equal(isLocalSourceHref("/content/%2e%2e/api/assistant"), false)
  assert.equal(isLocalSourceHref("/education/content/../../api"), false)
  assert.equal(isLocalSourceHref("/content/sub/../../api/assistant"), false)
  assert.equal(isLocalSourceHref("/content/..\\api"), false)
})

test("sse parser keeps safe sources, caps count, and reads error shapes", () => {
  const parsed = parseSseBlock(sseEvent("sources", {
    sources: [
      { id: "1", title: "Yerel", href: "/content/yerel", excerpt: "alıntı" },
      { id: "2", title: "Dış", href: "https://dis.test/x", excerpt: "yok" },
      { id: "3", title: "JS", href: "javascript:alert(1)", excerpt: "yok" },
      { id: "4", title: "Üst", href: "/content/../etc", excerpt: "yok" },
    ],
  }))
  assert.equal(parsed?.event, "sources")
  assert.deepEqual(parsed.sources.map((s) => s.href), ["/content/yerel"])
  const many = parseSseBlock(sseEvent("sources", {
    sources: Array.from({ length: 8 }, (_, i) => ({ id: `${i}`, title: `T${i}`, href: `/content/${i}`, excerpt: "" })),
  }))
  assert.equal(many.sources.length, 5)
  assert.equal(parseSseBlock(sseEvent("delta", { text: "merhaba" }))?.text, "merhaba")
  assert.equal(parseSseBlock(sseEvent("done", { grounded: true }))?.grounded, true)
  assert.match(parseSseBlock(sseEvent("error", { error: { message: "özel hata" } }))?.message, /özel hata/)
  assert.equal(parseSseBlock("not json\n\n"), null)
  assert.equal(parseSseBlock(": yorum\n\nevent: delta\ndata: {\"text\":\"x\"}\n\n")?.text, "x")
})

test("download controls and explicit auto-install intent match translation lane", () => {
  assert.deepEqual(downloadControlActions("queued"), ["pause", "cancel"])
  assert.deepEqual(downloadControlActions("downloading"), ["pause", "cancel"])
  assert.deepEqual(downloadControlActions("paused"), ["resume", "cancel"])
  assert.deepEqual(downloadControlActions("failed"), ["retry", "cancel"])
  assert.deepEqual(downloadControlActions("completed"), [])
  const done = downloadFixture({ status: "completed" })
  assert.equal(shouldAutoInstallProfile({ ...profileFixture({ status: "missing", download: done }) }, new Set(["dl-1"]), new Set()), true)
  assert.equal(shouldAutoInstallProfile({ ...profileFixture({ status: "missing", download: done }) }, new Set(), new Set()), false)
  assert.equal(shouldAutoInstallProfile({ ...profileFixture({ status: "missing", download: done }) }, new Set(["dl-1"]), new Set(["dl-1"])), false)
  assert.equal(shouldAutoInstallProfile({ ...profileFixture({ status: "ready", download: done }) }, new Set(["dl-1"]), new Set()), false)
  assert.equal(shouldAutoInstallProfile({ ...profileFixture({ status: "installing", download: done }) }, new Set(["dl-1"]), new Set()), false)
  const runtime = { available: true, status: "missing", download: done, error: null, version: "1.0" }
  assert.equal(shouldAutoInstallRuntime(runtime, new Set(["dl-1"]), new Set()), true)
  assert.equal(shouldAutoInstallRuntime(runtime, new Set(), new Set()), false)
  assert.equal(shouldAutoInstallRuntime({ ...runtime, status: "ready" }, new Set(["dl-1"]), new Set()), false)
  assert.equal(extractDownloadPayload({ download: done }), done)
  assert.equal(extractDownloadPayload({}), null)
  const profile = profileFixture()
  assert.equal(extractProfilePayload({ profile }), profile)
  assert.equal(extractProfilePayload({}), null)
})

test("utf-8 bytes and CRLF split across SSE chunks still stream", async () => {
  const full = sseEvent("status", { state: "generating" })
    + sseEvent("sources", { sources: [{ id: "k1", title: "Yerel kay", href: "/content/k1", excerpt: "özet" }] })
    + sseEvent("delta", { text: "😀 merhaba " })
    + sseEvent("delta", { text: "dünya" })
    + sseEvent("done", { grounded: true })
  const crlf = full.replaceAll("\n", "\r\n")
  const bytes = enc.encode(crlf)
  // Awkward splits: 1 byte, inside the emoji multibyte sequence, inside a CRLF pair.
  const cuts = [1, 5, 9]
  const chunks = []
  let start = 0
  for (const cut of cuts) {
    chunks.push(bytes.slice(start, cut))
    start = cut
  }
  chunks.push(bytes.slice(start))
  const restore = stubFetch(async () => chatResponse(chunks))
  const hook = mountChat()
  try {
    await hook.read().run("yerel soru", "lite")
    const turn = hook.read().turns[0]
    assert.equal(turn.status, "ready")
    assert.equal(turn.text, "😀 merhaba dünya")
    assert.equal(turn.sources.length, 1)
    assert.equal(turn.grounded, true)
    assert.equal(turn.interrupted, false)
  } finally { hook.unmount(); restore() }
})

test("streaming delta stays visible across rerenders (no per-render abort)", async () => {
  const stream = controllableSse()
  const restore = stubFetch(async () => ({
    ok: true,
    headers: { get: (n) => (n.toLowerCase() === "content-type" ? "text/event-stream" : null) },
    json: async () => null,
    body: { getReader: () => stream.reader },
  }))
  const hook = mountChat()
  try {
    const running = hook.read().run("soru", "lite")
    await flush(2)
    stream.push(sseEvent("delta", { text: "kısmi " }))
    await flush(2)
    // Rerender mid-stream: stable useCallback + dep-aware effects mean this
    // read must not cancel the in-flight request.
    assert.equal(hook.read().turns[0].text, "kısmi ")
    assert.equal(hook.read().streaming, true)
    assert.equal(hook.read().run, hook.read().run, "run must stay stable across rerenders")
    assert.equal(hook.read().stop, hook.read().stop, "stop must stay stable across rerenders")
    stream.push(sseEvent("delta", { text: "devam" }) + sseEvent("done", { grounded: false }))
    stream.finish()
    await running
    assert.equal(hook.read().turns[0].text, "kısmi devam")
    assert.equal(hook.read().turns[0].status, "ready")
  } finally { hook.unmount(); restore() }
})

test("missing done marks interrupted/error instead of clean success", async () => {
  const withPartial = chatResponse([enc.encode(sseEvent("delta", { text: "yarım" }))])
  const restore = stubFetch(async () => withPartial)
  const hook = mountChat()
  try {
    await hook.read().run("soru", "lite")
    const turn = hook.read().turns[0]
    assert.ok(turn.status === "ready" ? turn.interrupted === true : turn.status === "error")
    if (turn.status === "ready") assert.equal(turn.text, "yarım")
    else assert.match(turn.message, /yarıda|kesildi/)
  } finally { hook.unmount(); restore() }

  const empty = chatResponse([])
  const restore2 = stubFetch(async () => empty)
  const hook2 = mountChat()
  try {
    await hook2.read().run("soru", "lite")
    assert.equal(hook2.read().turns[0].status, "error")
    assert.match(hook2.read().turns[0].message, /yarıda|kesildi/)
  } finally { hook2.unmount(); restore2() }
})

test("stop aborts the request and retains the partial reply", async () => {
  const stream = controllableSse()
  let seenSignal = null
  const restore = stubFetch(async (_url, options) => {
    seenSignal = options?.signal ?? null
    return {
      ok: true,
      headers: { get: (n) => (n.toLowerCase() === "content-type" ? "text/event-stream" : null) },
      json: async () => null,
      body: { getReader: () => stream.reader },
    }
  })
  const hook = mountChat()
  try {
    const running = hook.read().run("durdur beni", "lite")
    await flush(2)
    stream.push(sseEvent("delta", { text: "başlangıç " }))
    await flush(3)
    assert.equal(hook.read().turns[0].text, "başlangıç ")
    hook.read().stop(true)
    assert.equal(seenSignal?.aborted, true)
    stream.finish()
    await running
    const turn = hook.read().turns[0]
    assert.equal(turn.text, "başlangıç ")
    assert.ok(turn.status === "ready" ? turn.interrupted === true : turn.status === "error")
  } finally { hook.unmount(); restore() }
})

test("unmount aborts the in-flight chat request", async () => {
  const stream = controllableSse()
  let seenSignal = null
  const restore = stubFetch(async (_url, options) => {
    seenSignal = options?.signal ?? null
    return {
      ok: true,
      headers: { get: (n) => (n.toLowerCase() === "content-type" ? "text/event-stream" : null) },
      json: async () => null,
      body: { getReader: () => stream.reader },
    }
  })
  const hook = mountChat()
  try {
    const running = hook.read().run("kapat", "lite")
    await flush(2)
    hook.unmount()
    assert.equal(seenSignal?.aborted, true)
    stream.finish()
    await running
  } finally { restore() }
})

test("newest request wins even when the transport ignores abort", async () => {
  const pending = []
  const restore = stubFetch((_url, _options) => new Promise((resolve, reject) => { pending.push({ resolve, reject }) }))
  const hook = mountChat()
  try {
    const first = hook.read().run("eski soru", "lite")
    const second = hook.read().run("yeni soru", "lite")
    assert.equal(pending[0].resolve && pending.length, 2)
    assert.equal(hook.read().turns.length, 2)
    pending[1].resolve(chatResponse([enc.encode(
      sseEvent("delta", { text: "YENİ" }) + sseEvent("done", { grounded: false }),
    )]))
    await second
    pending[0].resolve(chatResponse([enc.encode(
      sseEvent("delta", { text: "ESKİ" }) + sseEvent("done", { grounded: false }),
    )]))
    await first
    const fresh = hook.read().turns.find((t) => t.prompt === "yeni soru")
    assert.equal(fresh?.text, "YENİ")
    assert.equal(fresh?.status, "ready")
    const staleTurn = hook.read().turns.find((t) => t.prompt === "eski soru")
    assert.notEqual(staleTurn?.text, "YENİ")
  } finally { hook.unmount(); restore() }
})

test("error after sources keeps sources and never reports clean success", async () => {
  const chunks = [enc.encode(
    sseEvent("sources", { sources: [{ id: "k1", title: "Yerel", href: "/content/k1", excerpt: "özet" }] })
    + sseEvent("delta", { text: "kısmi " })
    + sseEvent("error", { error: { message: "üretim kesildi" } }),
  )]
  const restore = stubFetch(async () => chatResponse(chunks))
  const hook = mountChat()
  try {
    await hook.read().run("soru", "lite")
    const turn = hook.read().turns[0]
    assert.deepEqual(turn.sources.map((s) => s.href), ["/content/k1"])
    assert.ok(turn.status === "ready" ? turn.interrupted === true : /kesildi/.test(turn.message))
    if (turn.status === "ready") assert.equal(turn.text, "kısmi ")
  } finally { hook.unmount(); restore() }
})

test("unsafe source links never render and markup is escaped", () => {
  const evil = parseSseBlock(sseEvent("sources", {
    sources: [
      { id: "x", title: "<script>alert(1)</script>", href: "javascript:alert(1)", excerpt: "<img src=x onerror=alert(1)>" },
      { id: "y", title: "Dış", href: "https://dis.test/", excerpt: "" },
    ],
  }))
  assert.deepEqual(evil?.sources, [])
  const html = renderToStaticMarkup(React.createElement("a", { href: "/content/k1" }, "<script>alert(1)</script>"))
  assert.doesNotMatch(html, /<script/)
  assert.match(html, /&lt;script/)
  const view = renderToStaticMarkup(React.createElement(AssistantView, {}))
  assert.match(view, /ASİSTAN/)
  assert.match(view, /Asistan durumu yükleniyor/)
  assert.doesNotMatch(view, /javascript:/)
})

test("encoded-slash traversal is rejected by the href guard", () => {
  // "%2F" decodes to "/" on many servers: pathname stays "/content/..." here
  // only because the encoded slash is preserved, so the helper's
  // `url.pathname === href` shortcut accepts it. Must stay rejected.
  assert.equal(isLocalSourceHref("/content/%2F../api"), false)
  assert.equal(isLocalSourceHref("/content/%2f%2fapi"), false)
})

test("SourceList renders local sources and escapes hostile markup", () => {
  assert.equal(typeof SourceList, "function")
  assert.equal(renderToStaticMarkup(React.createElement(SourceList, { sources: [] })), "")
  const html = renderToStaticMarkup(React.createElement(SourceList, {
    sources: [{ id: "k1", title: "<script>alert(1)</script>", href: "/content/k1", excerpt: "<img src=x onerror=alert(1)>" }],
  }))
  assert.match(html, /Yerel kaynaklar/)
  assert.match(html, /href="\/content\/k1"/)
  assert.doesNotMatch(html, /<script/)
  assert.match(html, /&lt;script/)
  assert.doesNotMatch(html, /<img/)
})

test("in-memory transcript appends turns and reset clears them", async () => {
  const restore = stubFetch(async () => chatResponse([enc.encode(
    sseEvent("delta", { text: "yanıt" }) + sseEvent("done", { grounded: false }),
  )]))
  const hook = mountChat()
  try {
    await hook.read().run("birinci soru", "lite")
    await hook.read().run("ikinci soru", "balanced")
    const turns = hook.read().turns
    assert.equal(turns.length, 2)
    assert.ok(turns[1].id > turns[0].id)
    assert.equal(turns[0].prompt, "birinci soru")
    assert.equal(turns[0].profileId, "lite")
    assert.equal(turns[1].prompt, "ikinci soru")
    assert.equal(turns.every((t) => t.status === "ready"), true)
    hook.read().reset()
    assert.deepEqual(hook.read().turns, [])
    assert.equal(hook.read().streaming, false)
  } finally { hook.unmount(); restore() }
})

test("stop with no partial reply records an explicit stopped error", async () => {
  const stream = controllableSse()
  const restore = stubFetch(async () => ({
    ok: true,
    headers: { get: (n) => (n.toLowerCase() === "content-type" ? "text/event-stream" : null) },
    json: async () => null,
    body: { getReader: () => stream.reader },
  }))
  const hook = mountChat()
  try {
    const running = hook.read().run("soru", "lite")
    await flush(2)
    hook.read().stop(true)
    stream.finish()
    await running
    const turn = hook.read().turns[0]
    assert.equal(turn.status, "error")
    assert.match(turn.message, /Durduruldu/)
  } finally { hook.unmount(); restore() }
})

test("no effect depends on the per-render chat object", () => {
  const source = readFileSync(fileURLToPath(new URL("../src/assistant.tsx", import.meta.url)), "utf8")
  assert.doesNotMatch(
    source,
    /useEffect\([\s\S]*?\[chat\]/,
    "AssistantView must not use [chat] as an effect dep: chat is a new object every render and would abort streaming on each rerender",
  )
})
