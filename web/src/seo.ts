// 客户端动态 SEO 同步工具：配合服务端 HTML 注入，在 SPA 路由切换时同步标题、Meta、Open Graph、Twitter Card、Canonical 与 JSON-LD。

export interface PageSeoOptions {
  title: string
  description?: string
  robots?: string
  canonicalPath?: string
  ogType?: 'website' | 'article'
  siteName?: string
  image?: string
  publishedTime?: string | null
  modifiedTime?: string | null
  section?: string
  tags?: string[]
  jsonLd?: Record<string, unknown> | Record<string, unknown>[] | null
}

const JSON_LD_ATTR = 'data-buugoo-seo'
const DEFAULT_IMAGE = '/icon-512.png'

function safeUrl(raw?: string): string {
  const u = (raw ?? '').trim()
  if (!u) return ''
  if (u.startsWith('/') && !u.startsWith('//')) return u
  if (/^https?:\/\//i.test(u)) return u
  return ''
}

export function toAbsoluteUrl(pathOrUrl: string): string {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const u = pathOrUrl.trim()
  if (/^https?:\/\//i.test(u)) return u
  if (!origin) return u
  return u.startsWith('/') ? `${origin}${u}` : `${origin}/${u}`
}

function upsertMeta(attr: 'name' | 'property', key: string, content: string) {
  if (typeof document === 'undefined') return
  const selector = `meta[${attr}="${key}"]`
  let el = document.head.querySelector<HTMLMetaElement>(selector)
  if (!content) {
    el?.remove()
    return
  }
  if (!el) {
    el = document.createElement('meta')
    el.setAttribute(attr, key)
    document.head.appendChild(el)
  }
  el.setAttribute('content', content)
}

function upsertCanonical(href: string) {
  if (typeof document === 'undefined') return
  let el = document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]')
  if (!href) {
    el?.remove()
    return
  }
  if (!el) {
    el = document.createElement('link')
    el.setAttribute('rel', 'canonical')
    document.head.appendChild(el)
  }
  el.setAttribute('href', href)
}

function clearArticleMeta() {
  if (typeof document === 'undefined') return
  const keys = ['article:published_time', 'article:modified_time', 'article:section', 'article:tag']
  for (const key of keys) {
    document.head.querySelectorAll(`meta[property="${key}"]`).forEach((n) => n.remove())
  }
}

function setJsonLd(payload?: Record<string, unknown> | Record<string, unknown>[] | null) {
  if (typeof document === 'undefined') return
  document.head.querySelectorAll(`script[type="application/ld+json"][${JSON_LD_ATTR}]`).forEach((n) => n.remove())
  if (!payload) return
  const list = Array.isArray(payload) ? payload : [payload]
  for (const item of list) {
    const script = document.createElement('script')
    script.type = 'application/ld+json'
    script.setAttribute(JSON_LD_ATTR, '1')
    script.textContent = JSON.stringify(item).replace(/<\//g, '<\\/')
    document.head.appendChild(script)
  }
}

export function setPageSeo(opts: PageSeoOptions) {
  if (typeof document === 'undefined') return
  const siteName = (opts.siteName ?? '').trim() || '布谷'
  const title = opts.title.trim() || siteName
  const description = (opts.description ?? '').trim()
  const robots = (opts.robots ?? 'index, follow').trim()
  const ogType = opts.ogType ?? 'website'
  const canonical = opts.canonicalPath ? toAbsoluteUrl(opts.canonicalPath) : ''
  const image = toAbsoluteUrl(safeUrl(opts.image) || DEFAULT_IMAGE)

  document.title = title
  upsertMeta('name', 'description', description)
  upsertMeta('name', 'robots', robots)
  upsertCanonical(canonical)

  upsertMeta('property', 'og:type', ogType)
  upsertMeta('property', 'og:site_name', siteName)
  upsertMeta('property', 'og:locale', 'zh_CN')
  upsertMeta('property', 'og:title', title)
  upsertMeta('property', 'og:description', description)
  upsertMeta('property', 'og:url', canonical)
  upsertMeta('property', 'og:image', image)

  upsertMeta('name', 'twitter:card', 'summary_large_image')
  upsertMeta('name', 'twitter:title', title)
  upsertMeta('name', 'twitter:description', description)
  upsertMeta('name', 'twitter:image', image)

  clearArticleMeta()
  if (ogType === 'article') {
    if (opts.publishedTime) upsertMeta('property', 'article:published_time', opts.publishedTime)
    if (opts.modifiedTime) upsertMeta('property', 'article:modified_time', opts.modifiedTime)
    if (opts.section) upsertMeta('property', 'article:section', opts.section)
    if (opts.tags?.length) {
      for (const tag of opts.tags) {
        const clean = tag.trim()
        if (!clean) continue
        const el = document.createElement('meta')
        el.setAttribute('property', 'article:tag')
        el.setAttribute('content', clean)
        document.head.appendChild(el)
      }
    }
  }

  setJsonLd(opts.jsonLd ?? null)
}

export function resetPrivateSeo(siteName = '布谷') {
  if (typeof document === 'undefined') return
  const name = siteName.trim() || '布谷'
  document.title = name
  upsertMeta('name', 'robots', 'noindex, nofollow')
  upsertCanonical('')
  clearArticleMeta()
  setJsonLd(null)
}
