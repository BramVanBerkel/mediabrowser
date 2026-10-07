// Syntax highlighting with highlight.js. The core and each language load only
// when a code file or Markdown code block in that language is shown.

// File extensions and code block names, by highlight.js language.
const names = {
  bash: 'sh bash zsh fish ksh shell console',
  c: 'c h',
  cpp: 'cpp hpp cc cxx hh c++',
  css: 'css scss less',
  dart: 'dart',
  diff: 'diff patch',
  dos: 'bat cmd batch',
  go: 'go golang',
  graphql: 'graphql gql',
  ini: 'ini toml cfg conf properties env',
  java: 'java',
  javascript: 'js mjs cjs jsx javascript',
  json: 'json jsonc',
  kotlin: 'kt kts kotlin gradle',
  lua: 'lua',
  markdown: 'md markdown',
  perl: 'pl pm perl',
  php: 'php',
  powershell: 'ps1 psm1 powershell pwsh',
  protobuf: 'proto protobuf',
  python: 'py python',
  r: 'r',
  ruby: 'rb ruby',
  rust: 'rs rust',
  scala: 'scala',
  sql: 'sql',
  swift: 'swift',
  typescript: 'ts tsx mts cts typescript',
  xml: 'html htm xhtml xml svg vue svelte plist',
  yaml: 'yml yaml',
};

const languages = new Map();
for (const [lang, list] of Object.entries(names)) {
  for (const n of list.split(' ')) languages.set(n, lang);
}

// Languages whose grammar highlights code embedded in other languages.
const embeds = { xml: ['css', 'javascript'], markdown: ['xml'] };

const MAX_CHARS = 300_000; // larger files stay plain, as highlighting them would stall the page

let core = null;
const loading = new Map(); // language -> promise of hljs with it registered

function load(lang) {
  core ??= import('./vendor/highlight/core.min.js').then((m) => m.default);
  if (!loading.has(lang)) {
    loading.set(lang, (async () => {
      const [hljs, grammar] = await Promise.all([core, import(`./vendor/highlight/languages/${lang}.min.js`)]);
      hljs.registerLanguage(lang, grammar.default);
      await Promise.all((embeds[lang] || []).map(load));
      return hljs;
    })().catch((err) => {
      // Try again next time, e.g. after a dropped connection.
      loading.delete(lang);
      core = null;
      throw err;
    }));
  }
  return loading.get(lang);
}

// Highlights code written in name (a file extension or code block language).
// Resolves to HTML with the code escaped and wrapped in hljs-* spans, or to
// null when the language isn't known or the code is too long.
export async function highlight(code, name) {
  const lang = name && languages.get(name.toLowerCase());
  if (!lang || code.length > MAX_CHARS) return null;
  try {
    const hljs = await load(lang);
    return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
  } catch {
    return null;
  }
}
