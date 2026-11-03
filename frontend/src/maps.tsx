import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import * as maplibregl from "maplibre-gl"
import type { StyleSpecification } from "maplibre-gl"
import { PMTiles, Protocol } from "pmtiles"
import { DARK, layers, type Flavor } from "@protomaps/basemaps"
import maplibreWorkerUrl from "maplibre-gl/dist/maplibre-gl-worker.mjs?url"
import "maplibre-gl/dist/maplibre-gl.css"
import { formatBytes } from "./downloads"

// The worker file is resolved relative to the main bundle at runtime, which a
// bundled build cannot provide. Point MapLibre at the emitted worker asset so
// vector tiles keep parsing on a background thread while the UI stays smooth.
maplibregl.config.WORKER_URL = maplibreWorkerUrl

// Register once for the lifetime of the application, not once per React mount.
const protocol = new Protocol()
maplibregl.addProtocol("pmtiles", protocol.tile)

type MapStatus = "available" | "queued" | "preparing" | "downloading" | "verifying" | "ready" | "error"
type MapPackage = {
  id: string
  title: string
  bounds: [number, number, number, number]
  maxZoom: number
  version: string
  status: MapStatus
  sizeBytes?: number
  error?: string
}
const statusLabels: Record<MapStatus, string> = {
  available: "İndirilmedi", queued: "Bekliyor", preparing: "Hazırlanıyor", downloading: "İndiriliyor ve çıkarılıyor", verifying: "Doğrulanıyor", ready: "Kullanıma hazır", error: "İşlem başarısız",
}
const running = (status: MapStatus) => ["queued", "preparing", "downloading", "verifying"].includes(status)

async function mapsRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, { cache: "no-store", ...options })
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new Error(typeof body?.error?.message === "string" ? body.error.message : typeof body?.error === "string" ? body.error : "İşlem tamamlanamadı. Tekrar deneyin.")
  }
  if (response.status === 204) return undefined as T
  return await response.json() as T
}

// Protomaps v4 palette: contrast is deliberately retained for roads and labels.
const flavor: Flavor = {
  ...DARK,
  background: "#2a2a24", earth: "#34342c", water: "#344347", buildings: "#49463a",
  park_a: "#384735", park_b: "#40503a", wood_a: "#344330", wood_b: "#3b4b35", scrub_a: "#434a37", scrub_b: "#48503a",
  industrial: "#464039", hospital: "#50453d", school: "#4a4538", pedestrian: "#504b3e",
  minor_service: "#827b65", minor_a: "#978b71", minor_b: "#a1977e", minor_service_casing: "#34342c", minor_casing: "#34342c",
  major: "#c4a484", highway: "#cd9a6c", link: "#ad9478", other: "#8d856f", railway: "#a1987f", boundaries: "#9b735b",
  bridges_minor: "#a1977e", bridges_major: "#c4a484", bridges_highway: "#cd9a6c", bridges_link: "#ad9478", bridges_other: "#8d856f",
  roads_label_minor: "#e1d1a4", roads_label_major: "#f0dca9", roads_label_minor_halo: "#292a24", roads_label_major_halo: "#292a24",
  city_label: "#f0dfaf", city_label_halo: "#292a24", subplace_label: "#e6d7a3", subplace_label_halo: "#292a24",
  state_label: "#d2c39c", state_label_halo: "#292a24", country_label: "#e6d7a3", ocean_label: "#a9c0b8", address_label: "#d5c6a1", address_label_halo: "#292a24",
  landcover: { grassland: "#3e4936", barren: "#454438", urban_area: "#403e34", farmland: "#474938", glacier: "#737b72", scrub: "#414a37", forest: "#354632" },
}

function MapViewer({ item, onBack }: { item: MapPackage; onBack: () => void }) {
  const container = useRef<HTMLDivElement>(null)
  const [mapError, setMapError] = useState<string | null>(null)
  const [west, south, east, north] = item.bounds
  useEffect(() => {
    if (!container.current) return
    const sourceUrl = `${window.location.origin}/api/maps/${encodeURIComponent(item.id)}/file`
    protocol.add(new PMTiles(sourceUrl))
    const style: StyleSpecification = {
      version: 8,
      glyphs: `${window.location.origin}/maps/fonts/{fontstack}/{range}.pbf`,
      sprite: `${window.location.origin}/maps/sprites/dark`,
      sources: { protomaps: { type: "vector", url: `pmtiles://${sourceUrl}`, attribution: '<a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener noreferrer">© OpenStreetMap</a> · <a href="https://protomaps.com" target="_blank" rel="noopener noreferrer">Protomaps</a>' } },
      layers: layers("protomaps", flavor, { lang: "tr" }) as StyleSpecification["layers"],
    }
    const map = new maplibregl.Map({ container: container.current, style, center: [(west + east) / 2, (south + north) / 2], zoom: 8, maxZoom: item.maxZoom, attributionControl: false })
    map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right")
    map.addControl(new maplibregl.AttributionControl({ compact: false }), "bottom-right")
    map.fitBounds([[west, south], [east, north]], { padding: 36, duration: 0 })
    // The archive panel can change size after the canvas is created (for example
    // when a responsive window settles). Keep the WebGL drawing buffer in sync.
    const observer = new ResizeObserver(() => map.resize())
    observer.observe(container.current)
    const frame = requestAnimationFrame(() => map.resize())
    map.on("load", () => map.resize())
    map.on("error", (event) => {
      const detail = event.error instanceof Error ? `${event.error.name}: ${event.error.message}` : String(event.error ?? "bilinmeyen hata")
      console.error("MapLibre error:", event.error, { detail, sourceId: (event as { sourceId?: unknown }).sourceId })
      setMapError(`Harita verisi veya yerel görsel kaynaklar yüklenemedi (${detail}). Paketi kontrol edin.`)
    })
    return () => { cancelAnimationFrame(frame); observer.disconnect(); map.remove(); protocol.tiles.delete(sourceUrl) }
  }, [item.id, item.maxZoom, west, south, east, north])
  return <section aria-labelledby="map-viewer-heading">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div><p className="terminal-label">Yerel harita</p><h2 id="map-viewer-heading" className="mt-1 font-display text-4xl tracking-wide text-parchment">{item.title.toLocaleUpperCase("tr-TR")}</h2><p className="mt-1 text-sm text-sand/90">Yüklü paket · Çevrimdışı kullanılabilir</p></div>
      <button className="btn-ghost" type="button" onClick={onBack}>Arşive dön</button>
    </div>
    {mapError && <p role="alert" className="mt-4 rounded border border-blood/60 bg-blood/10 px-4 py-3 text-sm text-parchment">{mapError}</p>}
    <div ref={container} className="map-canvas mt-5 overflow-hidden rounded-md border border-sand/25" aria-label={`${item.title} haritası`} />
    <p className="mt-3 text-xs leading-5 text-sand/80">Haritayı sürükleyerek gezinin; yakınlaştırma düğmelerini veya klavyeyi kullanın.</p>
  </section>
}

export function MapsView() {
  const [packages, setPackages] = useState<MapPackage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const [viewingId, setViewingId] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const normalizedQuery = query.trim().toLocaleLowerCase("tr-TR")
  const visiblePackages = useMemo(() => {
    const sorted = [...packages].sort((a, b) => a.title.localeCompare(b.title, "tr"))
    if (!normalizedQuery) return sorted
    return sorted.filter((item) => item.title.toLocaleLowerCase("tr-TR").includes(normalizedQuery))
  }, [packages, normalizedQuery])
  const load = useCallback(async () => {
    try {
      const result = await mapsRequest<{ packages: MapPackage[] }>("/api/maps")
      if (!Array.isArray(result.packages)) throw new Error("Harita listesi okunamadı.")
      setPackages(result.packages)
      setError(null)
    } catch (e) { setError(e instanceof Error ? e.message : "Harita arşivine ulaşılamadı.") }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { void load(); const timer = window.setInterval(() => { if (!document.hidden) void load() }, 2000); return () => window.clearInterval(timer) }, [load])
  async function action(item: MapPackage, action: "download" | "cancel" | "delete") {
    setPending(true); setError(null)
    try {
      const url = `/api/maps/${encodeURIComponent(item.id)}${action === "delete" ? "" : `/${action}`}`
      await mapsRequest<unknown>(url, { method: action === "delete" ? "DELETE" : "POST" })
      await load()
    } catch (e) { setError(e instanceof Error ? e.message : "İşlem tamamlanamadı.") }
    finally { setPending(false) }
  }
  const viewing = packages.find(item => item.id === viewingId && item.status === "ready")
  if (viewing) return <MapViewer item={viewing} onBack={() => setViewingId(null)} />
  return <section aria-labelledby="maps-heading">
    <p className="terminal-label">Harita arşivi</p>
    <h2 id="maps-heading" className="mt-2 font-display text-4xl tracking-wide text-parchment">HARİTALAR</h2>
    <p className="mt-2 max-w-prose text-sm leading-6 text-sand/90">Bölge haritalarını bağlantı varken indirin, ihtiyaç duyduğunuzda çevrimdışı açın.</p>
    {!loading && packages.length > 0 && <div role="search" className="mt-6 w-full min-w-0 sm:max-w-md">
      <label htmlFor="il-arama" className="terminal-label">İl ara</label>
      <input id="il-arama" type="search" autoComplete="off" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="İl adı yazın, örn. Eskişehir" aria-describedby="il-sonuc-sayisi" className="field-terminal mt-2 min-w-0" />
      <p id="il-sonuc-sayisi" role="status" className="mt-2 font-mono text-xs text-sand/80">{normalizedQuery ? visiblePackages.length > 0 ? `${visiblePackages.length} il bulundu.` : "Aramanızla eşleşen il bulunamadı." : `${packages.length} il listeleniyor.`}</p>
    </div>}
    {error && <div role="alert" className="mt-6 rounded-md border border-blood/60 bg-blood/10 px-4 py-3 text-sm text-parchment">{error} <button type="button" className="ml-2 underline" onClick={() => void load()}>Yenile</button></div>}
    {loading ? <p role="status" className="mt-8 font-mono text-sm text-sand">Harita arşivi yükleniyor…</p> : packages.length === 0 ? <p className="mt-8 text-sm text-sand/90">Harita kataloğunda henüz bölge bulunmuyor.</p> : visiblePackages.length === 0 ? <p className="mt-8 max-w-prose text-sm leading-6 text-sand/90">Aramanızla eşleşen il bulunamadı. Farklı bir ad deneyin.</p> : <ul aria-label="İller" className="mt-8 divide-y divide-sand/15 border-y border-sand/15">{visiblePackages.map(item => <li key={item.id} className="grid gap-5 py-6 sm:grid-cols-[minmax(0,1fr)_auto]">
      <div className="min-w-0"><div className="flex flex-wrap items-center gap-3"><h3 className="break-words text-xl font-semibold text-parchment">{item.title}</h3><span className={`rounded px-2 py-1 font-mono text-xs ${item.status === "ready" ? "status-ok" : item.status === "error" ? "status-bad" : "bg-concrete text-parchment"}`}>{statusLabels[item.status]}</span></div>
        <p className="mt-2 text-sm text-sand/90">İl sınırlarını kapsayan bölge · Z0–Z{item.maxZoom}</p>
        <p className="mt-2 font-mono text-xs text-sand/80">{item.sizeBytes ? formatBytes(item.sizeBytes) : "Paket boyutu indirme sonrası belirlenir"} · {item.version}</p>
        {running(item.status) && <div className="mt-4 max-w-md"><progress className="download-progress" aria-label={statusLabels[item.status]} /><p className="mt-2 text-xs text-sand/80">İşlem devam ediyor. Kesin ilerleme bilgisi bulunmuyor.</p></div>}
        {item.status === "error" && <p role="alert" className="mt-3 max-w-prose text-sm text-parchment">{item.error || "Harita hazırlanamadı. Bağlantıyı kontrol edip tekrar deneyin."}</p>}
      </div><div className="flex flex-wrap content-start gap-2 sm:justify-end">
        {item.status === "ready" ? <><button className="btn-rust" type="button" onClick={() => setViewingId(item.id)}>Haritayı aç</button><button className="btn-ghost" type="button" disabled={pending} onClick={() => void action(item, "delete")}>Sil</button></> : running(item.status) ? <button className="btn-ghost" type="button" disabled={pending} onClick={() => void action(item, "cancel")}>İptal et</button> : <button className="btn-rust" type="button" disabled={pending} onClick={() => void action(item, "download")}>{item.status === "error" ? "Tekrar dene" : "İndir"}</button>}
      </div>
    </li>)}</ul>}
  </section>
}
