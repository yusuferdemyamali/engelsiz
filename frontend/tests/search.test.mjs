import assert from "node:assert/strict"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { test } from "node:test"
import { build } from "esbuild"

const require = createRequire(import.meta.url)
const React = require("react")
const compiled = await build({
  entryPoints: [fileURLToPath(new URL("../src/search.tsx", import.meta.url))],
  bundle: true,
  platform: "node",
  format: "cjs",
  external: ["react", "react/jsx-runtime"],
  write: false,
})
const module = { exports: {} }
new Function("require", "module", "exports", compiled.outputFiles[0].text)(require, module, module.exports)
const { useSearch } = module.exports

// Execute the real hook with a minimal React dispatcher; no browser or DOM is used.
function mountHook() {
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
      try { return useSearch() } finally { internal.current = previous }
    },
    unmount() { effects.forEach((cleanup) => cleanup?.()) },
  }
}

const packageA = "0123456789abcdef0123456789abcdef"
const packageB = "fedcba9876543210fedcba9876543210"
function response(query, packageId = packageA) {
  return { query, packageId, totalResults: 1, results: [{ title: query, snippet: "plain text", source: "Local", articleId: "A/Test", href: `/content/content/${packageId}/A/Test` }] }
}

function pendingFetch() {
  const original = globalThis.fetch
  const requests = []
  globalThis.fetch = (url, options) => new Promise((resolve, reject) => {
    requests.push({ url, signal: options.signal, resolve: (value) => resolve({ ok: true, json: async () => value }), reject })
  })
  return { requests, restore() { globalThis.fetch = original } }
}

test("only the newest query may publish even when the transport ignores abort", async () => {
  const fetch = pendingFetch()
  const hook = mountHook()
  try {
    const first = hook.read().run("old", packageA)
    const second = hook.read().run("new", packageA)
    assert.equal(fetch.requests[0].signal.aborted, true)
    assert.equal(hook.read().state.status, "loading")
    fetch.requests[1].resolve(response("new"))
    await second
    fetch.requests[0].resolve(response("old"))
    await first
    assert.equal(hook.read().state.response.query, "new")
  } finally { hook.unmount(); fetch.restore() }
})

test("a changed package invalidates the old request and uses exactly q/packageId", async () => {
  const fetch = pendingFetch()
  const hook = mountHook()
  try {
    const first = hook.read().run("wiki", packageA)
    hook.read().reset()
    const second = hook.read().run("  Türkçe & bilgi  ", packageB)
    const url = new URL(fetch.requests[1].url, "http://local")
    assert.deepEqual([...url.searchParams.keys()], ["q", "packageId"])
    assert.equal(url.searchParams.get("q"), "Türkçe & bilgi")
    assert.equal(url.searchParams.get("packageId"), packageB)
    fetch.requests[1].resolve(response("Türkçe & bilgi", packageB))
    await second
    fetch.requests[0].reject(new Error("late old failure"))
    await first
    assert.equal(hook.read().state.response.packageId, packageB)
  } finally { hook.unmount(); fetch.restore() }
})

test("reset and unmount cancel pending work without publishing stale results", async () => {
  const fetch = pendingFetch()
  const hook = mountHook()
  try {
    const first = hook.read().run("wiki", packageA)
    hook.read().reset()
    fetch.requests[0].resolve(response("wiki"))
    await first
    assert.equal(hook.read().state.status, "idle")
    const second = hook.read().run("later", packageA)
    hook.unmount()
    assert.equal(fetch.requests[1].signal.aborted, true)
    fetch.requests[1].resolve(response("later"))
    await second
    assert.equal(hook.read().state.status, "loading")
  } finally { fetch.restore() }
})

test("blank queries stay initial, zero results stay successful, unsafe hrefs fail explicitly", async () => {
  const fetch = pendingFetch()
  const hook = mountHook()
  try {
    await hook.read().run(" \u2003 ", packageA)
    assert.equal(fetch.requests.length, 0)
    assert.equal(hook.read().state.status, "idle")
    const empty = hook.read().run("nothing", packageA)
    fetch.requests[0].resolve({ query: "nothing", packageId: packageA, totalResults: 0, results: [] })
    await empty
    assert.equal(hook.read().state.status, "ready")
    const unsafe = hook.read().run("bad", packageA)
    const result = response("bad")
    result.results[0].href = "https://elsewhere.invalid/article"
    fetch.requests[1].resolve(result)
    await unsafe
    assert.equal(hook.read().state.status, "error")
  } finally { hook.unmount(); fetch.restore() }
})
