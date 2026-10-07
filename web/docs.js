// Documents the viewer can show: PDFs (in the browser's own PDF viewer) and
// text, code and Markdown files (fetched and shown as text, Markdown rendered).
import { el, enc } from './util.js';
import { highlight } from './syntax.js';

const textExts = new Set((
  'txt text log md markdown rst csv tsv ini cfg conf env properties ' +
  'js mjs cjs ts jsx tsx mts cts json jsonc html htm xhtml css scss less vue svelte go py rb java c h cpp hpp cc cxx hh ' +
  'rs sh bash zsh fish ksh ps1 psm1 bat cmd yml yaml toml xml plist php swift kt kts scala dart lua pl pm r sql ' +
  'graphql gql proto gradle tex srt vtt diff patch'
).split(' '));

const extOf = (name) => {
  const dot = name.lastIndexOf('.');
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
};

export const isMarkdown = (e) => ['md', 'markdown'].includes(extOf(e.name));

// What the viewer shows a file as: 'image', 'video', 'audio', 'pdf', 'text',
// or null for files it can't show.
export function viewKind(e) {
  if (e.type === 'image' || e.type === 'video' || e.type === 'audio') return e.type;
  if (e.type !== 'other') return null;
  const ext = extOf(e.name);
  if (ext === 'pdf') return 'pdf';
  return textExts.has(ext) ? 'text' : null;
}

export const MAX_TEXT = 1 << 20; // bytes of a text file the viewer loads

// Fetches up to limit bytes from the start of a text file. Resolves to
// { text, truncated } or { binary: true }, and throws if it can't be loaded.
export async function fetchText(e, limit = MAX_TEXT) {
  const truncated = e.size > limit;
  const res = await fetch('/media/' + enc(e.full), truncated ? { headers: { Range: `bytes=0-${limit - 1}` } } : {});
  if (!res.ok) throw new Error('HTTP ' + res.status);
  const bytes = new Uint8Array(await res.arrayBuffer());
  if (bytes.subarray(0, 8000).includes(0)) return { binary: true }; // text files have no NUL bytes
  return { text: new TextDecoder().decode(bytes), truncated };
}

/* ---------- Markdown ---------- */

const escapeHTML = (s) =>
  s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

// Where a link or image in the Markdown file at dir points to. Relative paths
// resolve to files next to it. Everything goes through the URL parser (as the
// browser would), and only web and mail links are kept, so no javascript: or
// data: URLs get through however they are spelled.
function safeHref(href, dir) {
  if (href.startsWith('#')) return href;
  try {
    const url = new URL(href, new URL('/media/' + enc(dir) + (dir ? '/' : ''), location.origin));
    if (url.origin === location.origin) return url.pathname + url.search + url.hash;
    return ['http:', 'https:', 'mailto:'].includes(url.protocol) ? url.href : '';
  } catch {
    return '';
  }
}

// Renders Markdown to HTML that is safe to insert: raw HTML in the file shows
// as text instead of being run, and links and images go through safeHref.
// Code blocks with a language get coloured once their grammar has loaded.
export async function renderMarkdown(text, dir) {
  const { Marked } = await import('./vendor/marked.esm.js');
  const md = new Marked({
    gfm: true,
    renderer: { html: ({ text }) => escapeHTML(text) },
    walkTokens(token) {
      if (token.type === 'link' || token.type === 'image') token.href = safeHref(token.href, dir);
    },
  });
  const box = el('div', 'markdown', md.parse(text));
  for (const a of box.querySelectorAll('a[href]')) {
    if (a.getAttribute('href').startsWith('#')) continue;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
  }
  for (const code of box.querySelectorAll('pre > code[class*="language-"]')) {
    const lang = code.className.match(/language-(\S+)/)[1];
    highlight(code.textContent, lang).then((html) => html && (code.innerHTML = html));
  }
  return box;
}
