import assert from "node:assert/strict"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { test } from "node:test"
import { build } from "esbuild"

const require = createRequire(import.meta.url)
const React = require("react")
const { renderToStaticMarkup } = require("react-dom/server")
const compiled = await build({
  entryPoints: [fileURLToPath(new URL("../src/translation.tsx", import.meta.url))],
  bundle: true,
  platform: "node",
  format: "cjs",
  external: ["react", "react/jsx-runtime"],
  write: false,
})
const module = { exports: {} }
new Function("require", "module", "exports", compiled.outputFiles[0].text)(require, module, module.exports)
const { countCharacters, validateTranslationInput, oppositeDirection, directionLabel, modelForDirection, useTranslate, useTranslationModels, ModelCard, TranslationView, downloadControlActions, extractDownloadPayload, extractModelPayload, shouldAutoInstall, canTranslateNow } = module.exports

// Execute the real hooks with a minimal React dispatcher; no browser or DOM is used.
function mountHookFor(hookFn) {
  const slots = []
  const effects = []
  let index = 0
  const dispatcher = {
    useState(initial) {
      const slot = index++
      if (!(slot in slots)) slots[slot] = typeof initial === "function" ? initial() : initial
      return [slots[slot], (value) => { slots[slot] = typeof value === "function" ? value(slots[slot]) : value }]
    },
    useRef(initial) {
      const slot = index++
      return slots[slot] ??= { current: initial }
    },
    useCallback(callback) { return callback },
    useEffect(callback) {
      const slot = index++
      if (!(slot in slots)) {
        slots[slot] = true
        effects.push(callback())
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
    unmount() { effects.forEach((cleanup) => cleanup?.()) },
  }
}

function mountHook() {
  return mountHookFor(useTranslate)
}

function pendingFetch() {
  const original = globalThis.fetch
  const requests = []
  globalThis.fetch = (url, options) => new Promise((resolve, reject) => {
    requests.push({ url, options, signal: options.signal, resolve: (value) => resolve({ ok: true, json: async () => value }), reject })
  })
  return { requests, restore() { globalThis.fetch = original } }
}

function stubDocument() {
  const original = globalThis.document
  globalThis.document = { visibilityState: "visible", addEventListener() {}, removeEventListener() {} }
  return () => { globalThis.document = original }
}

function flush() {
  return new Promise((resolve) => setImmediate(resolve))
}

function downloadFixture(overrides = {}) {
  return {
    id: "dl-1",
    provider: "argos",
    providerPackageId: "argos-tr-en",
    title: "Türkçe-İngilizce çeviri modeli",
    variant: "tr-en",
    fileName: "tr-en.argosmodel",
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

function modelFixture(overrides = {}) {
  return {
    id: "argos-tr-en",
    source_language: "tr",
    target_language: "en",
    version: "1.0",
    size_bytes: 1000,
    status: "missing",
    download: null,
    error: null,
    ...overrides,
  }
}

function overviewFixture(overrides = {}) {
  return { models: [modelFixture()], runtime_available: true, max_characters: 5000, ...overrides }
}

test("character count is unicode-aware instead of UTF-16 length", () => {
  assert.equal(countCharacters("Türkçe"), 6)
  assert.equal("😀".length, 2)
  assert.equal(countCharacters("😀"), 1)
  assert.equal(countCharacters("a😀b"), 3)
})

test("validation rejects blank and over-limit input before any request", () => {
  assert.match(validateTranslationInput("   ", 5000), /önce metin/)
  assert.match(validateTranslationInput("x".repeat(5001), 5000), /5000/)
  assert.match(validateTranslationInput("😀".repeat(5001), 5000), /5000/)
  assert.equal(validateTranslationInput("😀".repeat(5000), 5000), null)
  assert.equal(validateTranslationInput("merhaba dünya", 5000), null)
})

test("direction helpers resolve swap, labels and per-direction models", () => {
  assert.equal(oppositeDirection("tr-en"), "en-tr")
  assert.equal(oppositeDirection("en-tr"), "tr-en")
  assert.match(directionLabel("tr-en"), /Türkçe/)
  assert.match(directionLabel("en-tr"), /İngilizce/)
  const models = [
    { id: "argos-tr-en", source_language: "tr", target_language: "en" },
    { id: "argos-en-tr", source_language: "en", target_language: "tr" },
  ]
  assert.equal(modelForDirection(models, "tr-en")?.id, "argos-tr-en")
  assert.equal(modelForDirection(models, "en-tr")?.id, "argos-en-tr")
})

test("only the newest translation may publish even when the transport ignores abort", async () => {
  const fetch = pendingFetch()
  const hook = mountHook()
  try {
    const first = hook.read().run("eski metin", "tr", "en", 5000)
    const second = hook.read().run("yeni metin", "tr", "en", 5000)
    assert.equal(fetch.requests[0].signal.aborted, true)
    assert.equal(hook.read().state.status, "loading")
    const body = JSON.parse(fetch.requests[1].options.body)
    assert.equal(body.source_language, "tr")
    assert.equal(body.target_language, "en")
    assert.equal(body.text, "yeni metin")
    fetch.requests[1].resolve({ translated_text: "NEW" })
    await second
    fetch.requests[0].resolve({ translated_text: "OLD" })
    await first
    assert.equal(hook.read().state.status, "ready")
    assert.equal(hook.read().state.translatedText, "NEW")
  } finally { hook.unmount(); fetch.restore() }
})

test("invalid input fails without a request; reset clears results so edits never show stale text", async () => {
  const fetch = pendingFetch()
  const hook = mountHook()
  try {
    await hook.read().run("x".repeat(5001), "tr", "en", 5000)
    assert.equal(fetch.requests.length, 0)
    assert.equal(hook.read().state.status, "error")
    const pending = hook.read().run("merhaba", "tr", "en", 5000)
    fetch.requests[0].resolve({ translated_text: "hello" })
    await pending
    assert.equal(hook.read().state.translatedText, "hello")
    hook.read().reset()
    assert.equal(hook.read().state.status, "idle")
  } finally { hook.unmount(); fetch.restore() }
})

test("response wrappers accept only shaped download and model payloads", () => {
  const download = downloadFixture()
  assert.equal(extractDownloadPayload({ download }), download)
  assert.equal(extractDownloadPayload({}), null)
  assert.equal(extractDownloadPayload(null), null)
  assert.equal(extractDownloadPayload({ download: { status: "queued" } }), null)
  const model = modelFixture()
  assert.equal(extractModelPayload({ model }), model)
  assert.equal(extractModelPayload({}), null)
  assert.equal(extractModelPayload({ model: null }), null)
  assert.equal(extractModelPayload({ model: { status: "ready" } }), null)
})

test("download controls offer only recoverable transitions", () => {
  assert.deepEqual(downloadControlActions("queued"), ["pause", "cancel"])
  assert.deepEqual(downloadControlActions("downloading"), ["pause", "cancel"])
  assert.deepEqual(downloadControlActions("paused"), ["resume", "cancel"])
  assert.deepEqual(downloadControlActions("failed"), ["retry", "cancel"])
  assert.deepEqual(downloadControlActions("completed"), [])
  assert.deepEqual(downloadControlActions("cancelled"), [])
})

test("auto-install fires only for user-initiated completed downloads awaiting install", () => {
  const done = downloadFixture({ status: "completed" })
  assert.equal(shouldAutoInstall({ ...modelFixture(), status: "missing", download: done }, new Set(["dl-1"]), new Set()), true)
  assert.equal(shouldAutoInstall({ ...modelFixture(), status: "missing", download: done }, new Set(), new Set()), false)
  assert.equal(shouldAutoInstall({ ...modelFixture(), status: "missing", download: done }, new Set(["dl-1"]), new Set(["dl-1"])), false)
  assert.equal(shouldAutoInstall({ ...modelFixture(), status: "ready", download: done }, new Set(["dl-1"]), new Set()), false)
  assert.equal(shouldAutoInstall({ ...modelFixture(), status: "installing", download: done }, new Set(["dl-1"]), new Set()), false)
  assert.equal(shouldAutoInstall({ ...modelFixture(), status: "error", download: downloadFixture({ status: "failed" }) }, new Set(["dl-1"]), new Set()), false)
  assert.equal(shouldAutoInstall(modelFixture(), new Set(["dl-1"]), new Set()), false)
})

test("translate gating blocks doomed attempts without runtime, model, text or budget", () => {
  const overview = overviewFixture()
  const ready = modelFixture({ status: "ready" })
  assert.equal(canTranslateNow(overview, ready, "merhaba", 5000), true)
  assert.equal(canTranslateNow({ ...overview, runtime_available: false }, ready, "merhaba", 5000), false)
  assert.equal(canTranslateNow(overview, modelFixture(), "merhaba", 5000), false)
  assert.equal(canTranslateNow(overview, ready, "   ", 5000), false)
  assert.equal(canTranslateNow(overview, ready, "x".repeat(5001), 5000), false)
  assert.equal(canTranslateNow(null, ready, "merhaba", 5000), false)
  assert.equal(canTranslateNow(overview, undefined, "merhaba", 5000), false)
})

test("model download posts the wrapper endpoint and clears pending on a valid snapshot", async () => {
  const doc = stubDocument()
  const fetch = pendingFetch()
  const hook = mountHookFor(useTranslationModels)
  try {
    hook.read()
    assert.equal(fetch.requests[0].url, "/api/translation")
    fetch.requests[0].resolve(overviewFixture())
    await flush()
    assert.equal(hook.read().state.status, "ready")
    const model = hook.read().overview.models[0]
    const started = hook.read().startDownload(model)
    assert.match(fetch.requests[1].url, /\/api\/translation\/models\/argos-tr-en\/download/)
    assert.equal(fetch.requests[1].options.method, "POST")
    fetch.requests[1].resolve({ download: downloadFixture({ status: "downloading" }) })
    await started
    assert.equal(hook.read().pending, null)
    assert.equal(hook.read().actionError, null)
  } finally { hook.unmount(); fetch.restore(); doc() }
})

test("model install posts download_id and surfaces wrapper failures as retryable errors", async () => {
  const doc = stubDocument()
  const fetch = pendingFetch()
  const hook = mountHookFor(useTranslationModels)
  try {
    hook.read()
    fetch.requests[0].resolve(overviewFixture())
    await flush()
    const model = hook.read().overview.models[0]
    const installing = hook.read().installModel(model, "dl-9")
    assert.match(fetch.requests[1].url, /\/api\/translation\/models\/argos-tr-en\/install/)
    assert.equal(JSON.parse(fetch.requests[1].options.body).download_id, "dl-9")
    fetch.requests[1].resolve({ model: modelFixture({ status: "installing" }) })
    await installing
    assert.equal(hook.read().pending, null)
    assert.equal(hook.read().actionError, null)
    const broken = hook.read().startDownload(model)
    fetch.requests[2].resolve({})
    await broken
    assert.equal(hook.read().pending, null)
    assert.match(hook.read().actionError.key, /download/)
    assert.match(hook.read().actionError.message, /geçerli bir yanıt/)
  } finally { hook.unmount(); fetch.restore(); doc() }
})

test("failed downloads recover through the generic retry endpoint", async () => {
  const doc = stubDocument()
  const fetch = pendingFetch()
  const hook = mountHookFor(useTranslationModels)
  try {
    hook.read()
    fetch.requests[0].resolve(overviewFixture())
    await flush()
    const failed = { ...modelFixture(), download: downloadFixture({ status: "failed", error: { code: "kesinti", message: "Bağlantı kesildi." } }) }
    const retrying = hook.read().controlDownload(failed, "retry")
    assert.equal(fetch.requests[1].url, "/api/downloads/dl-1/retry")
    assert.equal(fetch.requests[1].options.method, "POST")
    fetch.requests[1].resolve({ download: downloadFixture({ status: "downloading" }) })
    await retrying
    assert.equal(hook.read().pending, null)
    assert.equal(hook.read().actionError, null)
  } finally { hook.unmount(); fetch.restore(); doc() }
})

function cardManager(overrides = {}) {
  return { pending: null, actionError: null, startDownload() {}, installModel() {}, controlDownload() {}, ...overrides }
}

function renderCard(model, manager = cardManager()) {
  return renderToStaticMarkup(React.createElement(ModelCard, { model, manager, onOpenDownloads() {} }))
}

test("model cards expose the recoverable action for each download state", () => {
  assert.match(renderCard(modelFixture()), /İndir/)
  assert.doesNotMatch(renderCard(modelFixture()), />Kur</)
  assert.match(renderCard({ ...modelFixture(), download: downloadFixture({ status: "completed" }) }), />Kur</)
  const failed = renderCard({ ...modelFixture(), download: downloadFixture({ status: "failed", error: { code: "kesinti", message: "Bağlantı kesildi." } }) })
  assert.match(failed, /Tekrar dene/)
  assert.match(failed, /İptal et/)
  assert.match(failed, /Bağlantı kesildi/)
  const active = renderCard({ ...modelFixture(), download: downloadFixture({ status: "downloading", downloadedBytes: 500, percentage: 50 }) })
  assert.match(active, /Duraklat/)
  assert.match(active, /<progress/)
  assert.match(active, /İndirmelerde takip et/)
  assert.match(renderCard({ ...modelFixture(), download: downloadFixture({ status: "paused" }) }), /Devam et/)
  const ready = renderCard({ ...modelFixture(), status: "ready" })
  assert.match(ready, /Kullanıma hazır/)
  assert.doesNotMatch(ready, /<button/)
  const installing = renderCard({ ...modelFixture(), status: "installing", download: downloadFixture({ status: "completed" }) })
  assert.match(installing, /Kuruluyor/)
  assert.match(installing, /Model kuruluyor/)
  assert.doesNotMatch(installing, />Kur</)
})

test("translation view renders heading and model loading state without a browser", () => {
  const html = renderToStaticMarkup(React.createElement(TranslationView, {}))
  assert.match(html, /ÇEVİRİ/)
  assert.match(html, /Çeviri modelleri yükleniyor/)
})
