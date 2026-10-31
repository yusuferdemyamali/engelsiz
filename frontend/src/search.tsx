import { useCallback, useEffect, useRef, useState, type FormEvent } from "react"
import { buttonClass, downloadRequest, secondaryButtonClass, variantsLabel } from "./downloads"
import type { Package } from "./packages"
import { RuntimeStatus, useRuntime } from "./runtime"

export type SearchResult = {
  title: string
  snippet: string
  source: string
  articleId: string
  href: string
}

export type SearchResponse = {
  query: string
  packageId: string
  totalResults: number
  results: SearchResult[]
}

export type SearchFailure = { error: { code: string; message: string } }

type SearchState =
  | { status: "idle" }
  | { status: "loading"; query: string }
  | { status: "ready"; response: SearchResponse }
  | { status: "error"; message: string; query: string }

type InstalledState =
  | { status: "loading" }
  | { status: "ready"; packages: Package[] }
  | { status: "error"; message: string }

function safeArticleHref(href: string, packageId: string) {
  const prefix = `/content/content/${packageId}/`
  if (!href.startsWith(prefix) || href.length === prefix.length || /[\\\u0000-\u0020\u007f?#]/u.test(href)) return false
  try {
    const url = new URL(href, "http://engelsiz.invalid")
    return url.origin === "http://engelsiz.invalid" && url.pathname.startsWith(prefix)
  } catch {
    return false
  }
}

function validResponse(value: SearchResponse, packageId: string, query: string) {
  return value?.packageId === packageId && value.query === query && Number.isSafeInteger(value.totalResults) && value.totalResults >= 0 &&
    Array.isArray(value.results) && value.results.length <= 25 && value.totalResults >= value.results.length &&
    value.results.every((item) => item && [item.title, item.snippet, item.source, item.articleId, item.href].every((field) => typeof field === "string") && safeArticleHref(item.href, packageId))
}

export function useSearch() {
  const [state, setState] = useState<SearchState>({ status: "idle" })
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

  const run = useCallback(async (input: string, packageId: string) => {
    cancel()
    const query = input.trim()
    if (!query || !packageId) {
      setState({ status: "idle" })
      return
    }
    if ([...query].length > 256 || /[\u0000-\u001f\u007f-\u009f]/u.test(query)) {
      setState({ status: "error", query, message: "En fazla 256 karakter içeren bir arama yazın; kontrol karakterlerini kaldırın." })
      return
    }
    const controller = new AbortController()
    active.current = controller
    setState({ status: "loading", query })
    try {
      const parameters = new URLSearchParams({ q: query, packageId })
      const result = await downloadRequest<SearchResponse>(`/api/search?${parameters}`, controller.signal)
      if (!validResponse(result, packageId, query)) throw new Error("Aramadan geçerli bir yanıt alınamadı. Tekrar deneyin.")
      if (!controller.signal.aborted && active.current === controller) setState({ status: "ready", response: result })
    } catch (failure) {
      if (!controller.signal.aborted && active.current === controller) {
        setState({ status: "error", query, message: failure instanceof Error ? failure.message : "Arama tamamlanamadı. Tekrar deneyin." })
      }
    } finally {
      if (active.current === controller) active.current = null
    }
  }, [cancel])

  return { state, run, reset }
}

const fieldClass = "min-h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-base text-foreground caret-primary placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
const countFormatter = new Intl.NumberFormat("tr-TR")

export function SearchView({ onOpenPackages }: { onOpenPackages: () => void }) {
  const [installed, setInstalled] = useState<InstalledState>({ status: "loading" })
  const [packageId, setPackageId] = useState("")
  const [query, setQuery] = useState("")
  const [refresh, setRefresh] = useState(0)
  const [article, setArticle] = useState<SearchResult | null>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const readerHeading = useRef<HTMLHeadingElement>(null)
  const restoreFocus = useRef(false)
  const search = useSearch()
  const runtime = useRuntime()

  useEffect(() => {
    const controller = new AbortController()
    setInstalled({ status: "loading" })
    void downloadRequest<{ packages: Package[] }>("/api/packages?status=installed", controller.signal).then((result) => {
      if (!Array.isArray(result.packages) || result.packages.some((item) => !item || item.status !== "installed" || !/^[a-f0-9]{32}$/.test(item.id) || typeof item.title !== "string")) {
        throw new Error("Kurulu paket listesi okunamadı. Tekrar deneyin.")
      }
      if (controller.signal.aborted) return
      const activePackages = result.packages.filter((item) => item.active)
      setInstalled({ status: "ready", packages: activePackages })
      setPackageId((current) => activePackages.some((item) => item.id === current) ? current : activePackages[0]?.id ?? "")
    }).catch((failure: unknown) => {
      if (!controller.signal.aborted) setInstalled({ status: "error", message: failure instanceof Error ? failure.message : "Kurulu paketler yüklenemedi. Tekrar deneyin." })
    })
    return () => controller.abort()
  }, [refresh])

  useEffect(() => {
    if (article) readerHeading.current?.focus()
    else if (restoreFocus.current) {
      heading.current?.focus()
      restoreFocus.current = false
    }
  }, [article])

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void search.run(query, packageId)
  }

  function returnToSearch() {
    restoreFocus.current = true
    setArticle(null)
  }

  const packages = installed.status === "ready" ? installed.packages : []
  const response = search.state.status === "ready" ? search.state.response : null

  return (
    <section className="pt-8 sm:pt-10" aria-labelledby={article ? "reader-heading" : "search-heading"}>
      <div hidden={article !== null}>
        <h2 ref={heading} id="search-heading" tabIndex={-1} className="text-2xl font-semibold tracking-tight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">Wikipedia’da ara</h2>
        <p className="mt-2 max-w-prose text-sm leading-6 text-muted-foreground">Kurulu paketlerinizde arayın ve makaleleri internet bağlantısı olmadan okuyun.</p>

        {installed.status === "loading" && <p role="status" className="mt-8 text-sm text-muted-foreground">Kurulu paketler yükleniyor…</p>}
        {installed.status === "error" && (
          <div className="mt-8 border-y py-6">
            <p role="alert" className="max-w-prose text-sm leading-6">{installed.message}</p>
            <button type="button" className={`${secondaryButtonClass} mt-4`} onClick={() => setRefresh((value) => value + 1)}>Paketleri tekrar yükle</button>
          </div>
        )}
        {installed.status === "ready" && packages.length === 0 && (
          <div className="mt-8 border-y py-6">
            <p role="status" className="max-w-prose text-sm leading-6 text-muted-foreground">Arama için henüz kurulu bir paket yok. İndirdiğiniz bir paketi doğrulayıp kurun.</p>
            <button type="button" className={`${secondaryButtonClass} mt-4`} onClick={onOpenPackages}>Yerel Paketlere git</button>
          </div>
        )}
        {packages.length > 0 && <>
          <RuntimeStatus manager={runtime} installedCount={packages.length} />
          <form onSubmit={submit} className="mt-8 space-y-5" role="search" aria-label="Kurulu Wikipedia paketinde ara">
            {packages.length > 1 ? (
              <div className="max-w-xl">
                <label htmlFor="search-package" className="mb-2 block text-sm font-medium">Aranacak paket</label>
                <select id="search-package" value={packageId} className={fieldClass} onChange={(event) => { search.reset(); setPackageId(event.target.value) }}>
                  {packages.map((item) => <option key={item.id} value={item.id}>{item.title} · {variantsLabel(item.variant)} · {item.version}</option>)}
                </select>
              </div>
            ) : <p className="break-words text-sm leading-6 text-muted-foreground">Aranacak paket: {packages[0].title} · {variantsLabel(packages[0].variant)} · {packages[0].version}</p>}
            <div>
              <label htmlFor="search-query" className="mb-2 block text-sm font-medium">Arama metni</label>
              <div className="flex flex-col gap-3 sm:flex-row">
                <input id="search-query" type="search" value={query} className={`${fieldClass} min-w-0 flex-1`} placeholder="Bir konu veya makale adı yazın" aria-describedby="search-help" onChange={(event) => { search.reset(); setQuery(event.target.value) }} />
                <button type="submit" className={`${buttonClass} shrink-0`} disabled={!query.trim() || !packageId}>Ara</button>
              </div>
              <p id="search-help" className="mt-2 text-sm leading-6 text-muted-foreground">En fazla 256 karakter. Aramayı başlatmak için Enter’a basın veya Ara’yı seçin.</p>
            </div>
          </form>

          <div className="mt-8" aria-busy={search.state.status === "loading"}>
            <p role="status" aria-atomic="true" className="text-sm leading-6 text-muted-foreground">
              {search.state.status === "idle" ? "Aramak istediğiniz konuyu yazın. Sonuçlar burada listelenecek."
                : search.state.status === "loading" ? `“${search.state.query}” aranıyor…`
                : response ? response.results.length === 0 ? `“${response.query}” için sonuç bulunamadı. Farklı bir kelime deneyin.`
                : `“${response.query}” için ${countFormatter.format(response.totalResults)} sonuç bulundu.${response.totalResults > response.results.length ? " İlk 25 sonuç gösteriliyor. Aramanızı daraltarak başka sonuçlara ulaşabilirsiniz." : ""}`
                : ""}
            </p>
            {search.state.status === "error" && (
              <div className="border-y py-6">
                <p role="alert" className="max-w-prose text-sm leading-6">{search.state.message}</p>
                <button type="button" className={`${secondaryButtonClass} mt-4`} onClick={() => void search.run(query, packageId)}>Aramayı tekrar dene</button>
              </div>
            )}
            {response && response.results.length > 0 && (
              <ul className="mt-4 divide-y border-y">
                {response.results.map((item) => (
                  <li key={item.articleId} className="py-6">
                    <h3 className="text-lg font-medium leading-7">
                      <a href={item.href} className="break-words underline decoration-border underline-offset-4 hover:decoration-foreground focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background" onClick={(event) => { if (event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) { event.preventDefault(); setArticle(item) } }}>{item.title}</a>
                    </h3>
                    {item.snippet && <p className="mt-2 max-w-prose break-words text-sm leading-6">{item.snippet}</p>}
                    {item.source && <p className="mt-2 break-words text-sm text-muted-foreground">Kaynak: {item.source}</p>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </>}
      </div>
      {article && (
        <div>
          <div className="mb-5 flex flex-wrap items-center justify-between gap-4">
            <h2 ref={readerHeading} id="reader-heading" tabIndex={-1} className="min-w-0 break-words text-xl font-semibold tracking-tight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">{article.title}</h2>
            <button type="button" className={`${secondaryButtonClass} shrink-0`} onClick={returnToSearch}>Aramaya dön</button>
          </div>
          <iframe title={`${article.title} — Wikipedia makalesi`} src={article.href} className="block h-[75vh] min-h-96 w-full rounded-md border bg-background" />
        </div>
      )}
    </section>
  )
}
