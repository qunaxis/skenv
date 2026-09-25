// VitePress config of the documentation site, https://qunaxis.github.io/skenv/.
// The pages are the Markdown files in docs/ as they are rendered on GitHub;
// this file only adds navigation and adapts GitHub-style links and anchors.
import { existsSync, readFileSync, statSync } from 'node:fs'
import { dirname, isAbsolute, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, type DefaultTheme } from 'vitepress'

const repo = 'https://github.com/qunaxis/skenv'
const docsDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(docsDir, '..')

// GitHub's heading ids (github-slugger), so that the `file.md#anchor` links
// written for GitHub also work on the site.
function githubSlug(s: string): string {
  return s
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{M}\p{N}\p{Pc}\- ]/gu, '')
    .replace(/ /g, '-')
}

const toPosix = (p: string) => p.split(sep).join('/')

// Directories of docs/ that are not site pages: the ADRs stay in the
// repository only, docs/public is copied as is (JSON Schemas, see
// public/schemas/README.md), and the demo sources are not documentation.
const excluded = ['adr/', 'public/', 'demo/']

// A relative link is resolved against the page, then against the repository
// root (README sections included into pages link to `docs/...`). A Markdown
// page of the site stays a site link; any other file in the repository
// (CHANGELOG.md, AGENTS.md, an ADR, docs/demo/demo.tape, ...) points to GitHub.
// Unknown targets are left alone, so the dead-link check reports them.
function rewriteHref(href: string, page: string): string {
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith('#') || href.startsWith('/')) {
    return href
  }
  const cut = href.search(/[?#]/)
  const path = cut < 0 ? href : href.slice(0, cut)
  const suffix = cut < 0 ? '' : href.slice(cut)
  if (!path) return href
  for (const target of [resolve(dirname(page), path), resolve(repoRoot, path)]) {
    if (!existsSync(target)) continue
    const inDocs = toPosix(relative(docsDir, target))
    const isPage =
      !inDocs.startsWith('..') && !isAbsolute(inDocs) && !excluded.some((dir) => inDocs.startsWith(dir))
    if (isPage && target.endsWith('.md')) {
      let rel = toPosix(relative(dirname(page), target))
      if (!rel.startsWith('.')) rel = './' + rel
      return rel + suffix
    }
    const kind = statSync(target).isDirectory() ? 'tree' : 'blob'
    return `${repo}/${kind}/main/${toPosix(relative(repoRoot, target))}${suffix}`
  }
  return href
}

// Placeholders such as SKENV_<KEY> or <owner>/<repo> in running text parse
// as HTML tags, which Vue then rejects. Inline tags that are not HTML
// elements are rendered as text.
const htmlTags = new Set(
  ('a abbr b br code del details div em h1 h2 h3 h4 h5 h6 hr i img ins kbd li ol p picture pre s ' +
    'samp small source span strong sub summary sup table tbody td th thead tr u ul var video').split(' '),
)
function isPlaceholder(html: string): boolean {
  const m = /^<\/?([A-Za-z][\w-]*)[\s/>]/.exec(html)
  return m !== null && !htmlTags.has(m[1].toLowerCase())
}

// The command reference sidebar follows the generated index, so a new or
// removed command needs no change here.
function commandItems(): DefaultTheme.SidebarItem[] {
  const index = readFileSync(resolve(docsDir, 'commands/README.md'), 'utf8')
  return [...index.matchAll(/^- \[([^\]]+)\]\(([^)]+)\.md\)/gm)].map(([, text, file]) => ({
    text,
    link: `/commands/${file}`,
  }))
}

const guide: DefaultTheme.SidebarItem[] = [
  { text: 'Install', link: '/install' },
  { text: 'Getting started', link: '/getting-started' },
  { text: 'Adopting an existing setup', link: '/adopting' },
  { text: 'The skenv file', link: '/skenv-file' },
  { text: 'Editor support', link: '/editor-support' },
  { text: 'Manifest', link: '/manifest' },
  { text: 'Git hosts', link: '/git-hosts' },
  { text: 'Project skills', link: '/project-skills' },
  { text: 'Harness', link: '/harness' },
  { text: 'Claude Code hook', link: '/claude-code-hook' },
]

const reference: DefaultTheme.SidebarItem[] = [
  { text: 'Commands', link: '/commands' },
  { text: 'Configuration', link: '/configuration' },
  { text: 'Command reference', link: '/commands/README', collapsed: true, items: commandItems() },
]

const contributing: DefaultTheme.SidebarItem[] = [
  { text: 'Contributing', link: '/contributing' },
  { text: 'Development and releases', link: '/releasing' },
  { text: 'Changelog', link: `${repo}/blob/main/CHANGELOG.md` },
]

export default defineConfig({
  title: 'skenv',
  description: 'One manifest, the same agent skills on every machine.',
  lang: 'en-US',
  base: '/skenv/',
  cleanUrls: true,
  // A link to a page that does not exist fails the build (the CI check).
  ignoreDeadLinks: false,
  srcExclude: excluded.map((dir) => `${dir}**`),
  head: [['meta', { name: 'theme-color', content: '#3c8772' }]],
  markdown: {
    anchor: { slugify: githubSlug },
    config(md) {
      // <!-- github-only --> ... <!-- /github-only --> is shown on GitHub
      // (pointers from pages that only include README sections), not here.
      md.core.ruler.after('block', 'skenv-github-only', (state) => {
        const marker = (i: number, text: string) =>
          state.tokens[i].type === 'html_block' && state.tokens[i].content.trim() === text
        for (let i = 0; i < state.tokens.length; i++) {
          if (!marker(i, '<!-- github-only -->')) continue
          let j = i
          while (j < state.tokens.length && !marker(j, '<!-- /github-only -->')) j++
          state.tokens.splice(i, j - i + 1)
          i--
        }
      })
      md.core.ruler.after('inline', 'skenv-links', (state) => {
        const page = (state.env as { path?: string }).path ?? ''
        for (const block of state.tokens) {
          for (const t of block.children ?? []) {
            if (t.type === 'html_inline' && isPlaceholder(t.content)) {
              t.type = 'text'
            } else if (t.type === 'link_open' && page) {
              const href = t.attrGet('href')
              if (href) t.attrSet('href', rewriteHref(href, page))
            }
          }
        }
      })
    },
  },
  transformPageData(pageData) {
    if (pageData.relativePath.startsWith('commands/')) {
      // Generated pages start with an h2 and cannot be edited on GitHub.
      if (!pageData.title && pageData.relativePath !== 'commands/README.md') {
        pageData.title = pageData.relativePath.slice('commands/'.length, -'.md'.length).replace(/_/g, ' ')
      }
      pageData.frontmatter.editLink = false
    }
  },
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/install', activeMatch: '^/(install|getting-started|skenv-file|editor-support|manifest|git-hosts|harness|claude-code-hook)' },
      { text: 'Reference', link: '/commands', activeMatch: '^/(commands|configuration)' },
      { text: 'Contributing', link: '/contributing', activeMatch: '^/(contributing|releasing)' },
      { text: 'Releases', link: `${repo}/releases` },
    ],
    sidebar: [
      { text: 'Guide', items: guide },
      { text: 'Reference', items: reference },
      { text: 'Contributing', items: contributing },
    ],
    outline: { level: [2, 3] },
    search: { provider: 'local' },
    socialLinks: [{ icon: 'github', link: repo }],
    editLink: {
      pattern: `${repo}/edit/main/docs/:path`,
      text: 'Edit this page on GitHub',
    },
    footer: {
      message: 'Generated from the Markdown in the skenv repository.',
    },
  },
})
