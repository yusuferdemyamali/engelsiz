import assert from "node:assert/strict"
import { fileURLToPath } from "node:url"
import { test } from "node:test"
import { build } from "vite"

test("the production map viewer emits and references its local MapLibre worker", async () => {
  const result = await build({
    root: fileURLToPath(new URL("..", import.meta.url)), configFile: false, logLevel: "error",
    build: { write: false, rollupOptions: { input: fileURLToPath(new URL("../src/maps.tsx", import.meta.url)) } },
  })
  const output = (Array.isArray(result) ? result : [result]).flatMap(bundle => bundle.output)
  const worker = output.find(file => /maplibre-gl-worker-.*\.mjs$/.test(file.fileName))
  assert.ok(worker, "MapLibre's worker must be emitted, not served by the SPA fallback")
  assert.equal(worker.type, "asset")
  assert.ok(worker.source.length > 1000, "the worker must contain real executable code")
  assert.ok(output.some(file => file.type === "chunk" && file.code.includes(worker.fileName)), "the viewer must use the emitted worker URL")
})
