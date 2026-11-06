import assert from "node:assert/strict"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { test } from "node:test"
import { build } from "esbuild"

const require = createRequire(import.meta.url)
const React = require("react")
const { renderToStaticMarkup } = require("react-dom/server")
const compiled = await build({
  entryPoints: [fileURLToPath(new URL("../src/education.tsx", import.meta.url))],
  bundle: true,
  platform: "node",
  format: "cjs",
  external: ["react", "react/jsx-runtime"],
  write: false,
})
const module = { exports: {} }
new Function("require", "module", "exports", compiled.outputFiles[0].text)(require, module, module.exports)
const {
  categoryLabel,
  formatLabel,
  statusLabel,
  languageLabel,
  downloadControlActions,
  controlActionLabel,
  resourceHasActiveWork,
  shouldAutoInstallResource,
  canOpenResource,
  needsSafetyCaveat,
  isDisasterMedicalMaterial,
  isReferenceOnly,
  filterResources,
  safetyCaveatDate,
  validEducationList,
  extractResourcePayload,
  useEducationResources,
  ResourceCard,
  LicenseDetails,
  ReaderAttribution,
  EducationSkeleton,
  EducationView,
} = module.exports

function downloadFixture(overrides = {}) {
  return {
    id: "dl-1",
    provider: "education",
    providerPackageId: "edu-1",
    title: "Afet El Kitabı",
    variant: "pdf",
    fileName: "afet.pdf",
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

function licenseFixture(overrides = {}) {
  return {
    name: "CC-BY-SA 4.0",
    url: "https://example.invalid/cc-by-sa",
    attribution: "Açık Ders",
    redistribution: "Paylaşılabilir",
    verified: true,
    ...overrides,
  }
}

function resourceFixture(overrides = {}) {
  return {
    id: "edu-1",
    title: "Afet El Kitabı",
    description: "Deprem öncesi ve sonrası yapılması gerekenler.",
    category: "disaster",
    source: "Açık Ders",
    sourceUrl: "https://example.invalid/afet",
    language: "tr",
    format: "pdf",
    sizeBytes: 1000,
    version: "2026-01-01",
    publishedAt: "2026-01-01T00:00:00Z",
    license: licenseFixture(),
    downloadUrl: "https://example.invalid/afet.pdf",
    downloadable: true,
    restriction: null,
    status: "missing",
    download: null,
    error: null,
    openHref: null,
    ...overrides,
  }
}

function cardManager(overrides = {}) {
  return {
    pending: null,
    actionError: null,
    startDownload() {},
    installResource() {},
    controlDownload() {},
    retryRuntime() {},
    removeResource() {},
    ...overrides,
  }
}

function renderCard(resource, manager = cardManager(), onOpenPackages) {
  return renderToStaticMarkup(React.createElement(ResourceCard, { resource, manager, onOpen() {}, onOpenPackages }))
}

function renderReaderAttribution(resource) {
  return renderToStaticMarkup(React.createElement(ReaderAttribution, { resource }))
}

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
    useMemo(factory) { return factory() },
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

function pendingFetch() {
  const original = globalThis.fetch
  const requests = []
  globalThis.fetch = (url, options) => new Promise((resolve, reject) => {
    requests.push({ url, options, signal: options?.signal, resolve: (value) => resolve({ ok: true, json: async () => value }), reject })
  })
  return {
    requests,
    resolveOk(index, value) {
      const entry = requests[index]
      entry.resolve(value)
    },
    restore() { globalThis.fetch = original },
  }
}

function stubDocument() {
  const original = globalThis.document
  const listeners = {}
  const doc = {
    visibilityState: "visible",
    addEventListener(type, fn) { listeners[type] = fn },
    removeEventListener(type) { delete listeners[type] },
    setHidden(hidden) {
      doc.visibilityState = hidden ? "hidden" : "visible"
      listeners.visibilitychange?.()
    },
  }
  globalThis.document = doc
  return { doc, restore() { globalThis.document = original } }
}

function flush() {
  return new Promise((resolve) => setImmediate(resolve))
}

test("category names match the requested turkish names", () => {
  assert.equal(categoryLabel("disaster"), "Afet ve İlk Yardım")
  assert.equal(categoryLabel("survival"), "Hayatta Kalma ve Pratik Bilgiler")
  assert.equal(categoryLabel("science"), "Matematik ve Fen")
  assert.equal(categoryLabel("interactive"), "İnteraktif Bilim Laboratuvarı")
  assert.equal(formatLabel("pdf"), "PDF")
  assert.equal(statusLabel("installed"), "Kullanıma hazır")
  assert.equal(languageLabel("tr"), "Türkçe")
  assert.equal(languageLabel("en"), "İngilizce")
  assert.equal(languageLabel(""), "Bilinmiyor")
  assert.equal(controlActionLabel("pause"), "Duraklat")
})

test("download controls offer only recoverable transitions", () => {
  assert.deepEqual(downloadControlActions("queued"), ["pause", "cancel"])
  assert.deepEqual(downloadControlActions("downloading"), ["pause", "cancel"])
  assert.deepEqual(downloadControlActions("paused"), ["resume", "cancel"])
  assert.deepEqual(downloadControlActions("failed"), ["retry", "cancel"])
  assert.deepEqual(downloadControlActions("completed"), [])
  assert.deepEqual(downloadControlActions("cancelled"), [])
})

test("medical caveat applies only to disaster medical material", () => {
  assert.equal(needsSafetyCaveat(resourceFixture({ category: "disaster", title: "İlk yardım çantası", restriction: null })), true)
  assert.equal(isDisasterMedicalMaterial(resourceFixture({ category: "disaster", description: "Tıbbi müdahale adımları" })), true)
  assert.equal(needsSafetyCaveat(resourceFixture({ category: "science", title: "MEB kitabı", restriction: "MEB izniyle kullanılabilir." })), false)
  assert.equal(needsSafetyCaveat(resourceFixture({ category: "disaster", title: "Deprem çantası", description: "Su ve battaniye.", restriction: "Boyut 10 MB." })), false)
  assert.equal(needsSafetyCaveat(resourceFixture()), false)
})

test("safety date never presents version as publication date", () => {
  assert.match(safetyCaveatDate(resourceFixture({ publishedAt: "2026-01-01T00:00:00Z" })), /2026/)
  assert.equal(safetyCaveatDate(resourceFixture({ publishedAt: null, version: "v9" })), null)
  assert.equal(safetyCaveatDate(resourceFixture({ publishedAt: "bozuk", version: "v9" })), null)
})

test("failed transfer shows manager retry only, failed install shows install retry", () => {
  const transferFailed = renderCard(resourceFixture({ status: "downloading", download: downloadFixture({ status: "failed", error: { code: "kesinti", message: "Bağlantı kesildi." } }) }))
  assert.match(transferFailed, /Tekrar dene/)
  assert.match(transferFailed, /İptal et/)
  assert.match(transferFailed, /Bağlantı kesildi/)
  const retryCount = (transferFailed.match(/Tekrar dene/g) || []).length
  assert.equal(retryCount, 1)
  assert.doesNotMatch(transferFailed, /Yeniden indir/)
  const failedInstall = renderCard(resourceFixture({ status: "failed", download: downloadFixture({ status: "completed", percentage: 100 }), error: { code: "kurulum", message: "Kurulum başarısız." } }))
  assert.match(failedInstall, /Kurulumu tekrar dene/)
  assert.doesNotMatch(failedInstall, /Yeniden indir/)
})

test("cards show language and license details for every resource", () => {
  const html = renderCard(resourceFixture({ language: "tr" }))
  assert.match(html, /Dil: Türkçe/)
  assert.match(html, /Lisans ve kaynak/)
  assert.match(html, /CC-BY-SA/)
  assert.match(html, /harici bağlantı/)
  const unverified = renderCard(resourceFixture({ license: licenseFixture({ verified: false }) }))
  assert.match(unverified, /doğrulanmadı/)
  assert.match(unverified, /Lisans ve kaynak/)
})

test("installed zim without openHref explains blocked open with runtime retry", () => {
  const blocked = renderCard(
    resourceFixture({ status: "installed", format: "zim", openHref: null, error: { code: "runtime", message: "İçerik hizmeti hazır değil." } }),
    cardManager(),
    () => {},
  )
  assert.match(blocked, /Kurulu içerik şu anda açılamıyor/)
  assert.match(blocked, /İçerik hizmetini tekrar dene/)
  assert.match(blocked, /Yerel Paketlere git/)
  assert.doesNotMatch(blocked, /İçeriği aç/)
})

test("reader attribution keeps the exact api sentence with license and source links", () => {
  const phetAttribution = "Simulation by PhET Interactive Simulations, University of Colorado Boulder, licensed under CC BY-NC 4.0 (https://phet.colorado.edu)."
  const html = renderReaderAttribution(resourceFixture({
    id: "phet-energy",
    title: "Enerji Çeşitleri ve Dönüşümleri",
    category: "interactive",
    format: "html",
    language: "tr",
    source: "PhET Interactive Simulations, University of Colorado Boulder",
    sourceUrl: "https://phet.colorado.edu/tr/simulations/energy-forms-and-changes",
    license: {
      name: "CC BY-NC 4.0",
      url: "https://creativecommons.org/licenses/by-nc/4.0/",
      attribution: phetAttribution,
      redistribution: "Yalnızca ticari olmayan kullanım.",
      verified: true,
    },
    restriction: "Yalnızca ticari olmayan eğitim kullanımı; ticari kullanım ayrı lisans gerektirir.",
  }))
  assert.match(html, /CC BY-NC 4\.0/)
  assert.match(html, /https:\/\/creativecommons\.org\/licenses\/by-nc\/4\.0\//)
  assert.ok(html.includes(phetAttribution))
  assert.match(html, /Orijinal kaynak/)
  assert.match(html, /harici bağlantı, internet gerekli/)
  assert.match(html, /Kullanım: Yalnızca ticari olmayan eğitim kullanımı/)
  assert.doesNotMatch(html, /Tıbbi öneri/)
})

test("shape validation never crashes on missing license or download", () => {
  assert.equal(validEducationList({ resources: [resourceFixture()] }), true)
  assert.equal(validEducationList({ resources: [] }), true)
  const noLicense = { ...resourceFixture() }
  delete noLicense.license
  assert.equal(validEducationList({ resources: [noLicense] }), false)
  const badDownload = resourceFixture({ download: { id: 5 } })
  assert.equal(validEducationList({ resources: [badDownload] }), false)
  assert.equal(validEducationList({ resources: [{ ...resourceFixture(), category: "nope" }] }), false)
  assert.equal(validEducationList(null), false)
  assert.equal(extractResourcePayload({ resource: resourceFixture() })?.id, "edu-1")
  assert.equal(extractResourcePayload({ resource: noLicense }), null)
  assert.equal(extractResourcePayload({}), null)
  assert.equal(extractResourcePayload(null), null)
})

test("loading skeleton is theme-native and matches card rhythm", () => {
  const html = renderToStaticMarkup(React.createElement(EducationSkeleton, {}))
  assert.match(html, /Eğitim içerikleri yükleniyor/)
  assert.match(html, /animate-pulse/)
  assert.match(html, /bg-concrete/)
})

test("education view renders heading and skeleton without a browser", () => {
  const html = renderToStaticMarkup(React.createElement(EducationView, {}))
  assert.match(html, /EĞİTİM MERKEZİ/)
  assert.match(html, /Eğitim içerikleri yükleniyor/)
  assert.match(html, /Afet ve İlk Yardım/)
  assert.match(html, /Hayatta Kalma ve Pratik Bilgiler/)
  assert.match(html, /Matematik ve Fen/)
  assert.match(html, /İnteraktif Bilim Laboratuvarı/)
  assert.doesNotMatch(html, /Survival/)
})

test("hook publishes the education list and downloads through the wrapper endpoint", async () => {
  const doc = stubDocument()
  const fetch = pendingFetch()
  const hook = mountHookFor(useEducationResources)
  try {
    hook.read()
    assert.equal(fetch.requests[0].url, "/api/education")
    fetch.resolveOk(0, { resources: [resourceFixture()] })
    await flush()
    await flush()
    assert.equal(hook.read().state.status, "ready")
    const started = hook.read().startDownload(hook.read().resources[0])
    assert.match(fetch.requests[1].url, /\/api\/education\/edu-1\/download/)
    fetch.resolveOk(1, { resource: resourceFixture({ status: "downloading", download: downloadFixture({ status: "downloading", percentage: 10 }) }) })
    await started
    await flush()
    assert.equal(hook.read().pending, null)
    assert.equal(hook.read().actionError, null)
  } finally { hook.unmount(); fetch.restore(); doc.restore() }
})

test("hook recovers failed transfers through the generic retry endpoint", async () => {
  const doc = stubDocument()
  const fetch = pendingFetch()
  const hook = mountHookFor(useEducationResources)
  try {
    hook.read()
    fetch.resolveOk(0, { resources: [resourceFixture({ status: "downloading", download: downloadFixture({ status: "failed", error: { code: "kesinti", message: "Bağlantı kesildi." } }) })] })
    await flush()
    await flush()
    const failed = { ...resourceFixture({ status: "downloading" }), download: downloadFixture({ status: "failed" }) }
    const retrying = hook.read().controlDownload(failed, "retry")
    assert.equal(fetch.requests[1].url, "/api/downloads/dl-1/retry")
    fetch.resolveOk(1, { download: downloadFixture({ status: "downloading" }) })
    await retrying
    assert.equal(hook.read().pending, null)
    assert.equal(hook.read().actionError, null)
  } finally { hook.unmount(); fetch.restore(); doc.restore() }
})

test("hook auto-installs user-initiated completed downloads once", async () => {
  const doc = stubDocument()
  const fetch = pendingFetch()
  const hook = mountHookFor(useEducationResources)
  try {
    hook.read()
    fetch.resolveOk(0, { resources: [resourceFixture({ status: "downloaded", download: downloadFixture({ status: "completed", percentage: 100 }) })] })
    await flush()
    await flush()
    const resource = hook.read().resources[0]
    assert.equal(hook.read().state.status, "ready")
    void hook.read().installResource(resource, "dl-1")
    assert.match(fetch.requests[1].url, /\/api\/education\/edu-1\/install/)
    assert.equal(JSON.parse(fetch.requests[1].options.body).download_id, "dl-1")
    fetch.resolveOk(1, { resource: resourceFixture({ status: "installing", download: downloadFixture({ status: "completed", percentage: 100 }) }) })
    await flush()
    await flush()
    assert.equal(hook.read().pending, null)
  } finally { hook.unmount(); fetch.restore(); doc.restore() }
})

test("stale list responses never publish after a visibility change", async () => {
  const doc = stubDocument()
  const fetch = pendingFetch()
  const hook = mountHookFor(useEducationResources)
  try {
    hook.read()
    assert.equal(fetch.requests.length, 1)
    doc.doc.setHidden(true)
    fetch.resolveOk(0, { resources: [resourceFixture()] })
    await flush()
    await flush()
    assert.equal(hook.read().state.status, "loading")
    doc.doc.setHidden(false)
    await flush()
    assert.equal(fetch.requests.length, 2)
    fetch.resolveOk(1, { resources: [resourceFixture()] })
    await flush()
    await flush()
    assert.equal(hook.read().state.status, "ready")
    assert.equal(hook.read().resources.length, 1)
  } finally { hook.unmount(); fetch.restore(); doc.restore() }
})
