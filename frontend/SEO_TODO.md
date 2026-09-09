# SEO TODO — otownmakerspace.dk

Audit of the live site on 2026-09-09. Items are ordered by impact on being found in search.
Branch: `seo/technical-foundations`. `[x]` = implemented on the branch, `[ ]` = still open.

## Crawl and index eligibility

- [x] **Remove the client-side language redirect.** `layouts/_default/baseof.html` sent every visitor
      without an `otm-lang` localStorage entry to `/da/…` via `location.replace`. Googlebot never has
      that entry, so every English URL rendered as a JavaScript redirect while its canonical and the
      sitemap said the opposite. Script deleted; the `onclick` that set the entry is gone from the
      language switcher in `layouts/_partials/shared/header.html`.
- [x] **Serve a robots.txt.** `/robots.txt` returned the 404 page. `enableRobotsTXT = true` in
      `config/_default/hugo.toml` plus `layouts/robots.txt` with the auth/checkout paths disallowed
      and a `Sitemap:` line.
- [x] **Emit hreflang in the HTML.** Only the sitemap carried alternates. `baseof.html` now emits one
      `<link rel="alternate" hreflang>` per translation plus `x-default` pointing at English.
      `languageCode` changed from `en-us` to `en` so `<html lang>`, the sitemap and the link tags agree.
- [x] **Stop the members subdomain from being indexed.** `members.otownmakerspace.dk` serves a
      byte-identical copy of the whole site and appears in search results. The static handler in
      `api/internal/http/router.go` now sends `X-Robots-Tag: noindex, nofollow` on the members host.
- [x] **Drop the thin taxonomy pages.** `/tags/*` and `/categories/` were in the sitemap but no
      template links to them. `disableKinds = ['taxonomy', 'term']`.

## Snippets and local search

- [x] **Meta descriptions on all 26 pages that had none.** Root cause was a Hugo scoping bug in
      `baseof.html` (`{{ $description := … }}` inside `else` shadowed the outer variable, so the
      fallback never applied). Resolution now lives in `layouts/_partials/func/page-description.html`
      (front matter → `.Summary` → site description) and is shared by the meta tag and Open Graph.
      Every work-area, getting-started, docs, team and news page got a hand-written `description:`
      in both `.md` and `.da.md`.
- [x] **Danish titles.** Every Danish page ended in the English "Community Workshop in Odense";
      `title` is now set under `[languages.da]`.
- [x] **Structured data.** `layouts/_partials/structured-data.html` emits a `LocalBusiness` +
      `WebSite` JSON-LD graph on the home page (address, geo, email, social profiles) from the new
      `[params.address]` and `[params.geo]` config tables.
- [x] **One `<h1>` on the home page.** The mobile hero heading in
      `layouts/_partials/bento-workspace.html` is a `<p>` now; the desktop bento keeps the `<h1>`.
- [x] **Image alt text on the home page.** Hero uses the new `hero_image_alt` i18n key; tiles reuse
      their label key.
- [x] **`og:locale`** now `en_GB` / `da_DK` instead of the invalid bare language code
      (`layouts/_partials/opengraph.html`).

## Delivery

- [x] **Compression.** The Go `http.FileServer` sends everything uncompressed. Traefik `compress`
      middleware attached to the apex and members routers in `infra/app/docker-compose.yml`.
- [x] **Cache-Control.** Nothing had a caching header. `router.go` now sends
      `immutable, max-age=1y` for Hugo's content-hashed (`_hu_`) images, one week for `/fonts/`, and
      `no-cache` (revalidate via Last-Modified → 304) for HTML, `styles.css` and JS.
- [x] **Self-host third-party assets.** Google Fonts (Roboto, Space Grotesk; latin + latin-ext
      woff2), Font Awesome 6.5.1 CSS + webfonts, Klaro 0.7 CSS + JS, htmx 1.9.12 and the two flag
      SVGs now live under `static/fonts`, `static/css/vendor`, `static/js/vendor` and
      `static/images/flags`. Five third-party origins removed from the critical path
      (fonts.googleapis.com, fonts.gstatic.com, cdnjs.cloudflare.com, cdn.jsdelivr.net,
      cdn.kiprotect.com, unpkg.com).
- [x] **www host.** `https://www.otownmakerspace.dk` failed TLS (certificate only covers the apex).
      A separate Traefik router `otm-www` with its own certificate 301s every www request to the
      apex. On a preview deployment where `www.<sub>.<domain>` has no DNS record, only that router's
      certificate fails, never the apex one.

## Not done on this branch, with reason

- [ ] **Git-based `lastmod` in the sitemap** (`enableGitInfo = true`). The Docker build copies only
      `frontend/` into the builder stage, so Hugo has no `.git` to read and the build would fail.
      Needs either `COPY .git` in `infra/app/Dockerfile` (cache-busting on every commit) or a
      `lastmod` front-matter convention.
- [ ] **Danish news.** `news/august-2026-update` and `news/new-partnership` have no `.da.md`, so the
      Danish sitemap `lastmod` is stale (February 2026). Content, not code.

## Manual, outside the repo

- [ ] Google Search Console: verify the property, submit `https://otownmakerspace.dk/sitemap.xml`,
      then watch the Pages report for "Page with redirect" and "Duplicate without user-selected
      canonical" going to zero after this branch deploys.
- [ ] Record a baseline before deploy: indexed pages, impressions and average position for
      "makerspace odense" and "laserskærer odense". Compare after four to eight weeks.
- [ ] Google Business Profile for Havnegade 57 with the same name, address and hours as the JSON-LD.
- [ ] Bing Webmaster Tools: import the Search Console property.
- [ ] Run PageSpeed Insights in the browser after deploy (the anonymous API quota is exhausted).
