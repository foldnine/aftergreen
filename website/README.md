# aftergreen.foldnine.dev

The AfterGreen website: one static page served by a Cloudflare Worker with
static assets only. No build step, no framework, no dependencies.

```text
website/
├── wrangler.jsonc   Worker "aftergreen-site", custom domain aftergreen.foldnine.dev
└── site/            served as-is
    ├── index.html, 404.html
    ├── install.sh   copy of ../scripts/install.sh (CI checks they match)
    ├── _headers     CSP and security headers (read by Cloudflare, not served)
    ├── styles/site.css
    ├── scripts/copy.js   copy-to-clipboard; the page works without it
    ├── assets/      copies of brand/logo/*.svg (CI checks they match)
    ├── fonts/       Inter and JetBrains Mono, subset woff2, SIL OFL 1.1
    ├── favicon.svg  copy of brand/logo/aftergreen-icon.svg; icon-*.png rendered from it
    ├── og.png       copy of brand/assets/github-social-preview.png
    └── robots.txt, sitemap.xml
```

Colours and type come from [`brand/`](../brand/). Don't add colours; slate and
signal green are not text-safe on ivory (see `brand/guidelines.md`).

## Copy rules

Every product claim must match the repositories at the time of writing:
release status, what the Action supports (HTTP only), flags and exit codes.
Don't show an install command until it works (Homebrew stays off the page
until `foldnine/homebrew-tap` is published).

When a CLI release is published, update `v0.1.0` everywhere in `index.html`
(hero kicker, Install section: Windows snippet, download links, verification
note; Open source section; JSON-LD `version`). When an Action release is
published, update the Action example and the Install section. The Install
workflow runs the documented commands on every OS.

## Run locally

```bash
cd website
npx wrangler dev           # applies _headers and 404 handling like production
```

## Deploy

```bash
cd website
npx wrangler deploy
```

The first deploy creates the proxied DNS record for `aftergreen.foldnine.dev`
and its certificate. It touches no other record in the zone. There is no
`workers.dev` address and no preview URLs.
