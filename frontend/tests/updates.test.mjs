import assert from "node:assert/strict"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { test } from "node:test"
import { build } from "esbuild"
const require = createRequire(import.meta.url)
const React = require("react")
const { renderToStaticMarkup } = require("react-dom/server")
const compiled = await build({ entryPoints: [fileURLToPath(new URL("../src/updates.tsx", import.meta.url))], bundle: true, platform: "node", format: "cjs", external: ["react", "react/jsx-runtime"], write: false })
const module = { exports: {} }
new Function("require", "module", "exports", compiled.outputFiles[0].text)(require, module, module.exports)
const { PackageUpdates, UpdateCheck } = module.exports
const item = { id: "base", active: true, variant: "mini", title: "Türkçe Wikipedia" }
const offer = { packageId: "base", installedVersion: "2026-08-01", candidateProviderPackageId: "upstream", candidateVersion: "2026-10-01", sizeBytes: null, state: "update_available" }
function manager(update, overrides = {}) {
  return { snapshot: { comparisonAvailable: true, updates: [update] }, downloads: [], loading: false, error: null, pending: null, actionError: null, perform() {}, reload() {}, ...overrides }
}
function render(update, packages = [], overrides = {}) {
  return renderToStaticMarkup(React.createElement(PackageUpdates, { item, packages, manager: manager(update, overrides), onOpenDownloads() {} }))
}
test("offers state versions and unknown size; only active packages offer explicit update", () => {
  const html = render(offer)
  assert.match(html, /2026-08-01/)
  assert.match(html, /2026-10-01/)
  assert.match(html, /Boyut bilgisi yok/)
  assert.match(html, /<button[^>]*>Güncelle<\/button>/)
  const retained = renderToStaticMarkup(React.createElement(PackageUpdates, { item: { ...item, active: false }, packages: [], manager: manager(offer) }))
  assert.doesNotMatch(retained, /<button/)
})
test("catalog failure preserves local update error and enables retry only for an installed candidate", () => {
  const operation = { ...offer, id: "op", downloadId: "transfer", candidatePackageId: "new", state: "failed", error: { code: "activation_failed", message: "Yeni sürüm açılamadı." } }
  const snapshot = { comparisonAvailable: false, updates: [operation], error: { code: "catalog_unavailable", message: "Katalog şu anda yenilenemiyor." } }
  const check = renderToStaticMarkup(React.createElement(UpdateCheck, { manager: manager(operation, { snapshot }) }))
  assert.match(check, /Katalog şu anda yenilenemiyor/)
  assert.doesNotMatch(check, /daha yeni bir sürüm bulunamadı/)
  const html = render(operation, [{ id: "new", status: "installed" }], { snapshot })
  assert.match(html, /role="alert"/)
  assert.match(html, /Yeni sürüm açılamadı/)
  assert.match(html, /Etkinleştirmeyi tekrar dene/)
  assert.match(html, /İndirme ve kurulum işlemlerine git/)
  assert.doesNotMatch(render(operation, [{ id: "new", status: "failed" }]), /Etkinleştirmeyi tekrar dene/)
})
test("progress and install guidance stay visible during transfer; activation has a live loading state", () => {
  const operation = { ...offer, id: "op", downloadId: "transfer", state: "downloading" }
  const transfer = { id: "transfer", downloadedBytes: 500, totalBytes: 1000, percentage: 50, status: "completed", error: null }
  const html = render(operation, [], { downloads: [transfer] })
  assert.match(html, /<progress[^>]*value="50"/)
  assert.match(html, /Doğrula ve kur/)
  const activating = render({ ...operation, state: "activating" })
  assert.match(activating, /aria-busy="true"/)
  assert.match(activating, /role="status"/)
  assert.doesNotMatch(activating, /Yeni sürümü etkinleştir/)
})
test("success reports previous-file preservation without another update or retry action", () => {
  const html = render({ ...offer, id: "op", state: "succeeded" })
  assert.match(html, /Önceki sürüm cihazınızda korunuyor/)
  assert.doesNotMatch(html, /<button/)
})

test("cancelled update has an explicit preserved-version status and no activation control", () => {
  const html = render({ ...offer, id: "op", state: "cancelled" })
  assert.match(html, /Güncelleme iptal edildi/)
  assert.match(html, /Önceki sürüm korunuyor/)
  assert.doesNotMatch(html, /Etkinleştirmeyi tekrar dene/)
})
