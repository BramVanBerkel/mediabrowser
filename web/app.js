import { icon, fillIcons } from './icons.js';
import { $, el, enc, join, formatBytes, formatDate, barPeaks } from './util.js';
import { openViewer, closeViewer, viewerOpen } from './viewer.js';

fillIcons();

const items = $('#items');
const crumbs = $('#crumbs');
const details = $('#details');
const uploadsBox = $('#uploads');
const searchInput = $('#search');
const sortSelect = $('#sort');
const sortDirBtn = $('#sort-dir');

let current = '';   // folder being shown, relative to the root ('' = root)
let rootName = '';
let shown = [];     // entries of the folder or search, as received
let sorted = [];    // the same entries in display order
let message = '';   // empty state, or a note below the results
let selected = null; // entry shown in the details panel; null = the current folder
let detailsOpen = false;
let stats = new Map(); // folder stats by path, cleared on every load

/* ---------- File kinds ---------- */

// The server tells images, videos and audio apart; the rest is by extension,
// for icons and the Type column.
const extKinds = {};
for (const [kind, exts] of Object.entries({
  document: 'pdf doc docx txt md rtf odt pages epub key ppt pptx odp',
  spreadsheet: 'xls xlsx csv tsv ods numbers',
  code: 'js mjs ts jsx tsx json html htm css go py rb java c h cpp hpp rs sh yml yaml toml xml php swift kt sql',
  archive: 'zip rar 7z tar gz tgz bz2 xz dmg iso',
})) {
  for (const ext of exts.split(' ')) extKinds[ext] = kind;
}

function kindOf(e) {
  if (e.type === 'dir') return 'folder';
  if (e.type !== 'other') return e.type;
  const dot = e.name.lastIndexOf('.');
  return (dot > 0 && extKinds[e.name.slice(dot + 1).toLowerCase()]) || 'other';
}

const kindLabel = (e) => {
  const k = kindOf(e);
  return k[0].toUpperCase() + k.slice(1);
};

const kindIcons = {
  folder: 'folder', image: 'image', video: 'file-video', document: 'file-text', spreadsheet: 'file-spreadsheet',
  code: 'file-code', audio: 'file-audio', archive: 'file-archive', other: 'file',
};
const kindIcon = (e) => icon(kindIcons[kindOf(e)], 'kind-icon ' + kindOf(e));

const previewSrc = (e, p) => '/thumb/' + enc(join(e.full, p.path)) + '?v=' + p.mtime;

function thumbSrc(e) {
  // Audio only has a thumbnail if it carries cover art; otherwise the icon shows.
  if (e.type === 'image' || e.type === 'video' || e.type === 'audio') return '/thumb/' + enc(e.full) + '?v=' + e.mtime;
  if (e.type === 'dir' && e.preview && e.preview.length) return previewSrc(e, e.preview[0]);
  return null;
}

const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;

/* ---------- Loading ---------- */

// The hash holds the folder and an optional search: #/some/folder?q=beach
function parseHash() {
  const h = location.hash.replace(/^#\/?/, '');
  const i = h.indexOf('?');
  const q = i < 0 ? '' : new URLSearchParams(h.slice(i + 1)).get('q') || '';
  let folder = '';
  try {
    folder = decodeURIComponent(i < 0 ? h : h.slice(0, i)).replace(/\/+$/, '');
  } catch {}
  return { folder, q };
}

const hashFor = (folder, q) => '#/' + enc(folder) + (q ? '?q=' + encodeURIComponent(q) : '');

let loadSeq = 0;
async function load() {
  const { folder, q } = parseHash();
  const seq = ++loadSeq;
  if (document.activeElement !== searchInput) searchInput.value = q;
  const url = q
    ? '/api/search?path=' + encodeURIComponent(folder) + '&q=' + encodeURIComponent(q)
    : '/api/list?path=' + encodeURIComponent(folder);
  let res, data;
  try {
    res = await fetch(url);
    if (res.ok) data = await res.json();
  } catch {
    return show([], 'Could not reach the server.');
  }
  if (seq !== loadSeq) return; // a newer load started meanwhile
  if (res.status === 401) return (location.href = '/login');
  selected = null;
  stats = new Map();
  if (!res.ok) {
    current = folder;
    renderCrumbs(rootName || 'Home', '');
    return show([], res.status === 404 ? 'Folder not found.' : 'Could not open this folder.');
  }
  current = data.path;
  rootName = data.root;
  const folderName = current ? current.split('/').pop() : data.root;
  document.title = (q ? `“${q}” in ` : '') + folderName + ' · Media Browser';
  searchInput.placeholder = 'Search ' + folderName + '…';
  renderCrumbs(data.root, q);
  if (q) {
    show(data.results,
      !data.results.length ? `No files match “${q}”.`
        : data.truncated ? `Showing the first ${data.results.length} matches.` : '');
  } else {
    show(data.entries, data.entries.length ? '' : 'This folder is empty.');
  }
}

function show(entries, msg) {
  shown = entries;
  message = msg;
  render();
}

function renderCrumbs(rootLabel, q) {
  crumbs.replaceChildren();
  if (q) {
    const s = el('span', 'searching');
    const query = el('b');
    query.textContent = `“${q}”`;
    const where = el('b');
    where.textContent = current ? current.split('/').pop() : rootLabel;
    s.append('Search results for ', query, ' in ', where);
    crumbs.append(s);
    return;
  }
  const parts = current ? current.split('/') : [];
  const add = (label, path) => {
    if (crumbs.childNodes.length) crumbs.insertAdjacentHTML('beforeend', icon('chevron-right'));
    const a = el('a');
    a.textContent = label;
    a.href = '#/' + enc(path);
    crumbs.append(a);
  };
  add(rootLabel, '');
  parts.forEach((p, i) => add(p, parts.slice(0, i + 1).join('/')));
  crumbs.scrollLeft = crumbs.scrollWidth;
}

/* ---------- Sorting and view ---------- */

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
const byName = (a, b) => collator.compare(a.name, b.name) || collator.compare(a.path || '', b.path || '');
const sorters = {
  name: byName,
  modified: (a, b) => a.mtime - b.mtime || byName(a, b),
  size: (a, b) => a.size - b.size || byName(a, b),
  type: (a, b) => collator.compare(kindLabel(a), kindLabel(b)) || byName(a, b),
};

let sortKey = 'name';
let sortDir = 'asc';
let view = 'grid';
try {
  // Older versions saved one of six combined sort orders.
  const legacy = {
    'name-desc': ['name', 'desc'], newest: ['modified', 'desc'], oldest: ['modified', 'asc'],
    largest: ['size', 'desc'], smallest: ['size', 'asc'],
  };
  const saved = localStorage.getItem('sort');
  if (legacy[saved]) [sortKey, sortDir] = legacy[saved];
  else if (sorters[saved]) sortKey = saved;
  if (localStorage.getItem('sortDir') === 'desc') sortDir = 'desc';
  if (localStorage.getItem('view') === 'list') view = 'list';
} catch {}

function savePrefs() {
  try {
    localStorage.setItem('sort', sortKey);
    localStorage.setItem('sortDir', sortDir);
    localStorage.setItem('view', view);
  } catch {}
}

function renderControls() {
  sortSelect.value = sortKey;
  sortDirBtn.innerHTML = icon(sortDir === 'asc' ? 'arrow-up' : 'arrow-down');
  sortDirBtn.setAttribute('aria-label', sortDir === 'asc' ? 'Sort descending' : 'Sort ascending');
  for (const v of ['grid', 'list']) {
    const b = $('#view-' + v);
    b.classList.toggle('active', view === v);
    b.setAttribute('aria-pressed', view === v);
  }
}

sortSelect.addEventListener('change', () => {
  sortKey = sortSelect.value;
  savePrefs();
  render();
});
sortDirBtn.addEventListener('click', () => {
  sortDir = sortDir === 'asc' ? 'desc' : 'asc';
  savePrefs();
  render();
});
for (const v of ['grid', 'list']) {
  $('#view-' + v).addEventListener('click', () => {
    view = v;
    savePrefs();
    render();
  });
}

/* ---------- Items ---------- */

const viewable = () => sorted.filter((e) => e.type === 'image' || e.type === 'video' || e.type === 'audio');

function render() {
  renderControls();
  const cmp = sorters[sortKey];
  const dir = sortDir === 'asc' ? 1 : -1;
  // Folders always come first.
  sorted = [...shown].sort((a, b) => (b.type === 'dir') - (a.type === 'dir') || dir * cmp(a, b));
  // Search results carry a path relative to the current folder.
  for (const e of sorted) e.full = join(current, e.path || e.name);

  const empty = !sorted.length;
  $('#empty').hidden = !empty;
  $('#empty-text').textContent = message;
  $('#note').hidden = empty || !message;
  $('#note').textContent = message;
  items.hidden = empty;
  items.replaceChildren(empty ? '' : view === 'grid' ? renderGrid() : renderList());

  const folders = sorted.filter((e) => e.type === 'dir').length;
  $('#count').hidden = empty;
  $('#count').textContent = `${plural(folders, 'folder')}, ${plural(sorted.length - folders, 'file')}`;
  renderDetails();
}

// The link that opens an item. It is stretched over its card or row by CSS.
function openLink(e, cls) {
  const a = el('a', 'open ' + cls);
  a.title = e.path || e.name;
  if (e.type === 'dir') {
    a.href = '#/' + enc(e.full);
    return a;
  }
  a.href = '/media/' + enc(e.full);
  a.addEventListener('click', (ev) => {
    if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey) return; // let the browser open it
    ev.preventDefault();
    if (e.type === 'other') return select(e); // no viewer for these, show details instead
    const list = viewable();
    openViewer(list, list.indexOf(e));
  });
  return a;
}

function infoButton(e) {
  const b = el('button', 'icon-btn info-btn', icon('info'));
  b.type = 'button';
  b.title = `Information about ${e.name}`;
  b.setAttribute('aria-label', b.title);
  b.addEventListener('click', (ev) => {
    ev.preventDefault();
    ev.stopPropagation(); // never opens the item itself
    select(e);
  });
  return b;
}

// Thumbnail image that falls back to the kind icon if there is none. Audio
// without cover art tries its waveform first.
function thumbImg(e, onFail) {
  const src = thumbSrc(e);
  if (!src) return null;
  const img = el('img', 'thumb-img');
  img.loading = 'lazy';
  img.decoding = 'async';
  img.alt = '';
  img.src = src;
  img.onerror = e.type !== 'audio' ? onFail : () =>
    waveThumb(e).then((wave) => {
      if (!wave) onFail();
      else if (img.isConnected) img.replaceWith(wave);
    });
  return img;
}

const WAVE_BARS = 40; // bars in a waveform thumbnail

// An audio file's waveform as an SVG, coloured by CSS so it follows the theme.
// Resolves to null if the server has none (no ffmpeg, or an unreadable file).
async function waveThumb(e) {
  const res = await fetch('/waveform/' + enc(e.full) + '?v=' + e.mtime).catch(() => null);
  const data = res && res.ok ? await res.json().catch(() => null) : null;
  if (!data || !data.peaks || !data.peaks.length) return null;
  // Bars are 3 units wide with a 1 unit gap, in a 100 unit tall box the SVG
  // stretches to fill its thumbnail.
  const rects = barPeaks(data.peaks, WAVE_BARS).map((v, i) => {
    const h = Math.max(2, v);
    return `<rect x="${i * 4}" y="${(100 - h) / 2}" width="3" height="${h}"/>`;
  });
  return el('span', 'thumb-img thumb-wave',
    `<svg viewBox="0 0 ${WAVE_BARS * 4 - 1} 100" preserveAspectRatio="none" aria-hidden="true">${rects.join('')}</svg>`);
}

// A folder's cover made of up to four of its thumbnails. Tiles that fail to
// load stay empty; if all of them fail, onFail shows the folder icon instead.
function folderMosaic(e, onFail) {
  const items = e.preview.slice(0, 4);
  const mosaic = el('div', 'mosaic n' + items.length);
  let failed = 0;
  for (const p of items) {
    const img = el('img', 'thumb-img');
    img.loading = 'lazy';
    img.decoding = 'async';
    img.alt = '';
    img.src = previewSrc(e, p);
    img.onerror = () => {
      img.replaceWith(el('span', 'tile-empty'));
      if (++failed === items.length) onFail();
    };
    mosaic.append(img);
  }
  return mosaic;
}

const folderOf = (e) => (e.path && e.path.includes('/') ? e.path.slice(0, e.path.lastIndexOf('/')) : '');

function renderGrid() {
  const grid = el('div', 'grid');
  for (const e of sorted) {
    const card = el('div', 'card');
    e.node = card;
    if (e === selected) card.classList.add('selected');

    const thumb = el('div', 'thumb');
    const plain = () => {
      thumb.classList.add('plain');
      thumb.innerHTML = kindIcon(e);
    };
    const img = e.type === 'dir' && e.preview && e.preview.length > 1 ? folderMosaic(e, plain) : thumbImg(e, plain);
    if (img) {
      thumb.append(img);
      if (e.type === 'video') thumb.append(el('span', 'play-badge', '<span>' + icon('play') + '</span>'));
      if (e.type === 'dir') thumb.append(el('span', 'folder-badge', icon('folder')));
      if (e.type === 'audio') thumb.append(el('span', 'folder-badge audio', icon('music')));
    } else {
      plain();
    }

    const meta = el('div', 'meta');
    const row = el('div', 'name-row');
    const link = openLink(e, 'name');
    link.textContent = e.name;
    row.append(link, infoButton(e));
    const sub = el('span', 'sub');
    sub.textContent = e.type === 'dir' ? 'Folder' : formatBytes(e.size);
    meta.append(row, sub);
    const where = folderOf(e);
    if (where) {
      const w = el('span', 'sub where');
      w.textContent = where;
      meta.append(w);
    }
    card.append(thumb, meta);
    grid.append(card);
  }
  return grid;
}

function renderList() {
  const list = el('div', 'list');
  const head = el('div', 'row head');
  head.innerHTML = '<span>Name</span><span class="col-size">Size</span><span class="col-date">Modified</span>';
  list.append(head);
  for (const e of sorted) {
    const row = el('div', 'row');
    e.node = row;
    if (e === selected) row.classList.add('selected');

    const cell = el('div', 'cell-name');
    const icn = () => el('span', 'thumb-box', kindIcon(e));
    const img = thumbImg(e, () => img.replaceWith(icn()));
    const link = openLink(e, 'label-text');
    const n = el('span', 'n');
    n.textContent = e.name;
    link.append(n);
    const where = folderOf(e);
    if (where) {
      const w = el('span', 'where');
      w.textContent = where;
      link.append(w);
    }
    cell.append(img || icn(), link, infoButton(e));

    const size = el('span', 'col-size');
    size.textContent = e.type === 'dir' ? '—' : formatBytes(e.size);
    const date = el('span', 'col-date');
    date.textContent = formatDate(e.mtime);
    row.append(cell, size, date);
    list.append(row);
  }
  return list;
}

/* ---------- Details panel ---------- */

function select(e) {
  if (selected && selected.node) selected.node.classList.remove('selected');
  selected = e;
  if (e.node) e.node.classList.add('selected');
  detailsOpen = true;
  renderDetails();
}

function closeDetails() {
  if (selected && selected.node) selected.node.classList.remove('selected');
  selected = null;
  detailsOpen = false;
  renderDetails();
}

function renderDetails() {
  details.hidden = !detailsOpen;
  if (!detailsOpen) return;
  // Without a selection the panel describes the folder being shown.
  const e = selected || {
    name: current ? current.split('/').pop() : rootName,
    type: 'dir',
    full: current,
  };
  const isDir = e.type === 'dir';

  const head = el('div', 'd-head', '<span class="label">Details</span>');
  const close = el('button', 'icon-btn', icon('x'));
  close.type = 'button';
  close.setAttribute('aria-label', 'Close details');
  close.onclick = closeDetails;
  head.append(close);

  const preview = el('div', 'd-preview');
  const img = thumbImg(e, () => (preview.innerHTML = kindIcon(e)));
  if (img) preview.append(img);
  else preview.innerHTML = kindIcon(e);

  const body = el('div', 'd-body');
  const name = el('p', 'd-name');
  name.textContent = e.name;
  const dl = el('dl');
  const row = (label, value) => {
    const div = el('div');
    const dt = el('dt');
    dt.textContent = label;
    const dd = el('dd');
    dd.textContent = value;
    div.append(dt, dd);
    dl.append(div);
    return dd;
  };
  row('Type', kindLabel(e));
  const size = row('Size', isDir ? 'Calculating…' : formatBytes(e.size));
  const contains = isDir ? row('Contains', 'Calculating…') : null;
  const modified = row('Modified', e.mtime ? formatDate(e.mtime) : '…');
  const parent = e.full.includes('/') ? e.full.slice(0, e.full.lastIndexOf('/')) : '';
  row('Location', e.full ? [rootName, ...(parent ? parent.split('/') : [])].join(' / ') : '—');

  const dlBtn = el('a', 'dl-btn', icon('download'));
  dlBtn.href = isDir ? '/zip/' + enc(e.full) : '/media/' + enc(e.full) + '?download';
  dlBtn.download = '';
  dlBtn.append(isDir ? 'Download as ZIP' : 'Download');
  body.append(name, dl, dlBtn);
  details.replaceChildren(head, preview, body);

  if (isDir) {
    loadStats(e.full).then((st) => {
      if (!st || !size.isConnected) return;
      const pre = st.partial ? 'At least ' : '';
      size.textContent = pre + formatBytes(st.size);
      contains.textContent = pre + `${plural(st.folders, 'folder')}, ${plural(st.files, 'file')}`;
      modified.textContent = formatDate(st.mtime);
    });
  }
}

// Fetches a folder's total size and counts once per load; null if it fails.
function loadStats(path) {
  if (!stats.has(path)) {
    stats.set(path, fetch('/api/stats?path=' + encodeURIComponent(path))
      .then((res) => (res.ok ? res.json() : null))
      .catch(() => null));
  }
  return stats.get(path);
}

/* ---------- Theme ---------- */

const themeBtn = $('#theme-btn');
const themeMenu = $('#theme-menu');
const systemDark = matchMedia('(prefers-color-scheme: dark)');
let theme = 'system';
try {
  const saved = localStorage.getItem('theme');
  if (saved === 'light' || saved === 'dark') theme = saved;
} catch {}

function applyTheme() {
  document.documentElement.classList.toggle('dark', theme === 'dark' || (theme === 'system' && systemDark.matches));
  themeBtn.innerHTML = icon(theme === 'system' ? 'monitor' : theme === 'dark' ? 'moon' : 'sun');
  themeBtn.title = 'Theme: ' + theme;
  for (const b of themeMenu.children) b.setAttribute('aria-checked', b.dataset.theme === theme);
}
applyTheme();
systemDark.addEventListener('change', applyTheme);

function setMenu(open) {
  themeMenu.hidden = !open;
  themeBtn.setAttribute('aria-expanded', open);
  if (open) themeMenu.querySelector('[aria-checked="true"]').focus();
}
themeBtn.addEventListener('click', () => setMenu(themeMenu.hidden));
themeMenu.addEventListener('click', (ev) => {
  const b = ev.target.closest('button');
  if (!b) return;
  theme = b.dataset.theme;
  try {
    localStorage.setItem('theme', theme);
  } catch {}
  applyTheme();
  setMenu(false);
  themeBtn.focus();
});
themeMenu.addEventListener('keydown', (ev) => {
  const opts = [...themeMenu.children];
  const i = opts.indexOf(document.activeElement);
  if (ev.key === 'Escape') {
    setMenu(false);
    themeBtn.focus();
  } else if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
    opts[(i + (ev.key === 'ArrowDown' ? 1 : opts.length - 1)) % opts.length].focus();
  } else {
    return;
  }
  ev.preventDefault();
});
document.addEventListener('click', (ev) => {
  if (!themeMenu.hidden && !ev.target.closest('.theme')) setMenu(false);
});

/* ---------- Navigation ---------- */

window.addEventListener('popstate', () => closeViewer(true));
window.addEventListener('hashchange', () => {
  closeViewer(true);
  load();
});

/* ---------- Search ---------- */

let searchTimer;
function applySearch() {
  clearTimeout(searchTimer);
  const q = searchInput.value.trim();
  const { folder, q: oldQ } = parseHash();
  if (q === oldQ) return;
  // Starting a search adds a history entry (so Back leaves it); refining it doesn't.
  if (oldQ) history.replaceState(null, '', hashFor(folder, q));
  else history.pushState(null, '', hashFor(folder, q));
  load();
}
searchInput.addEventListener('input', () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(applySearch, 250);
});
searchInput.addEventListener('keydown', (ev) => {
  if (ev.key === 'Enter') {
    applySearch();
    searchInput.blur();
  } else if (ev.key === 'Escape') {
    searchInput.value = '';
    applySearch();
  }
});
document.addEventListener('keydown', (ev) => {
  const typing = ev.target.closest && ev.target.closest('input, select, textarea');
  if (ev.key === '/' && !viewerOpen() && !typing) {
    ev.preventDefault();
    searchInput.focus();
  }
});

/* ---------- Uploads ---------- */

let activeUploads = 0;

// Uploads items of { file, dir } to the current folder, dir being the
// subfolder (possibly nested, '' for none) the file goes into.
async function uploadFiles(items) {
  if (!items.length) return;
  const dest = current;
  uploadsBox.hidden = false;
  const rows = items.map(({ file, dir }) => {
    const row = el('div', 'up');
    const n = el('span', 'n');
    n.textContent = join(dir, file.name);
    const s = el('span', 's');
    s.textContent = 'Waiting · ' + formatBytes(file.size);
    const bar = el('div', 'bar', '<div></div>');
    row.append(n, s, bar);
    uploadsBox.append(row);
    return { file, dir, row, s, fill: bar.firstChild };
  });

  activeUploads++;
  for (const r of rows) await uploadOne(r, dest);
  activeUploads--;

  if (dest === current) load();
  setTimeout(() => {
    rows.forEach((r) => r.row.classList.contains('done') && r.row.remove());
    if (!uploadsBox.children.length) uploadsBox.hidden = true;
  }, 4000);
}

function uploadOne(r, dest) {
  return new Promise((resolve) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/upload?path=' + encodeURIComponent(dest));
    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable) return;
      const pct = Math.round((e.loaded / e.total) * 100);
      r.fill.style.width = pct + '%';
      r.s.textContent = pct + '%';
    };
    const fail = (msg) => {
      r.row.classList.add('error');
      r.s.textContent = msg;
      resolve(false);
    };
    xhr.onload = () => {
      if (xhr.status === 401) return fail('Not logged in');
      if (xhr.status !== 200) return fail('Failed');
      r.row.classList.add('done');
      r.s.textContent = 'Done';
      resolve(true);
    };
    xhr.onerror = () => fail('Failed');
    const fd = new FormData();
    fd.append('lastModified', r.file.lastModified); // lets the server keep the original date
    if (r.dir) fd.append('dir', r.dir); // the server creates it if needed
    fd.append('file', r.file, r.file.name);
    r.s.textContent = '0%';
    xhr.send(fd);
  });
}

const isHidden = (name) => name.startsWith('.'); // .DS_Store and the like

$('#file-input').addEventListener('change', (ev) => {
  uploadFiles([...ev.target.files].map((file) => ({ file, dir: '' })));
  ev.target.value = '';
});

// A picked folder's files come with paths like "Trip/Day 1/photo.jpg".
$('#folder-input').addEventListener('change', (ev) => {
  const items = [];
  for (const file of ev.target.files) {
    const parts = file.webkitRelativePath.split('/');
    if (parts.some(isHidden)) continue;
    items.push({ file, dir: parts.slice(0, -1).join('/') });
  }
  uploadFiles(items);
  ev.target.value = '';
});

// Collects the files below a dropped file or folder entry into out.
async function walkEntry(entry, dir, out) {
  if (isHidden(entry.name)) return;
  if (entry.isFile) {
    const file = await new Promise((res, rej) => entry.file(res, rej)).catch(() => null);
    if (file) out.push({ file, dir });
  } else if (entry.isDirectory) {
    const reader = entry.createReader();
    const sub = join(dir, entry.name);
    // readEntries returns the folder's contents in batches, then an empty one.
    for (;;) {
      const batch = await new Promise((res, rej) => reader.readEntries(res, rej)).catch(() => []);
      if (!batch.length) break;
      for (const e of batch) await walkEntry(e, sub, out);
    }
  }
}

async function droppedItems(dt) {
  // The entries must be taken before the drop event returns.
  const entries = [...dt.items].map((i) => i.webkitGetAsEntry?.()).filter(Boolean);
  if (!entries.length) {
    // No folder support: plain files only. Folders show up as empty, typeless files.
    return [...dt.files].filter((f) => f.size > 0 || f.type).map((file) => ({ file, dir: '' }));
  }
  const out = [];
  for (const e of entries) await walkEntry(e, '', out);
  return out;
}

window.addEventListener('beforeunload', (ev) => {
  if (activeUploads) ev.preventDefault();
});

// Drag and drop anywhere on the page.
const drop = $('#drop');
let dragDepth = 0;
const hasFiles = (ev) => ev.dataTransfer && [...ev.dataTransfer.types].includes('Files');
window.addEventListener('dragenter', (ev) => {
  if (!hasFiles(ev)) return;
  dragDepth++;
  drop.hidden = false;
});
window.addEventListener('dragleave', (ev) => {
  if (!hasFiles(ev)) return;
  if (--dragDepth <= 0) {
    dragDepth = 0;
    drop.hidden = true;
  }
});
window.addEventListener('dragover', (ev) => {
  if (hasFiles(ev)) ev.preventDefault();
});
window.addEventListener('drop', (ev) => {
  if (!hasFiles(ev)) return;
  ev.preventDefault();
  dragDepth = 0;
  drop.hidden = true;
  droppedItems(ev.dataTransfer).then(uploadFiles);
});

load();
