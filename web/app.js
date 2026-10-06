'use strict';

const $ = (sel) => document.querySelector(sel);
const grid = $('#grid');
const message = $('#message');
const crumbs = $('#crumbs');
const viewer = $('#viewer');
const stage = $('#v-stage');
const uploadsBox = $('#uploads');
const searchInput = $('#search');
const sortSelect = $('#sort');

const icons = {
  dir: '<svg viewBox="0 0 24 24"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
  image: '<svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="10" r="2"/><path d="m21 16-5-5-9 9"/></svg>',
  video: '<svg viewBox="0 0 24 24"><rect x="3" y="5" width="14" height="14" rx="2"/><path d="m17 10 4-2v8l-4-2"/></svg>',
  other: '<svg viewBox="0 0 24 24"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></svg>',
  play: '<svg viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>',
};

let current = '';      // folder being shown, relative to the root ('' = root)
let viewable = [];     // images and videos in the current folder, for the lightbox
let viewIndex = -1;

const enc = (p) => p.split('/').map(encodeURIComponent).join('/');
const join = (dir, name) => (dir ? dir + '/' + name : name);
const el = (tag, cls, html) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (html) e.innerHTML = html;
  return e;
};

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
    return showMessage('Could not reach the server.');
  }
  if (seq !== loadSeq) return; // a newer load started meanwhile
  if (res.status === 401) return (location.href = '/login');
  if (!res.ok) {
    current = folder;
    renderCrumbs('Home');
    grid.replaceChildren();
    return showMessage(res.status === 404 ? 'Folder not found.' : 'Could not open this folder.');
  }
  current = data.path;
  const folderName = current ? current.split('/').pop() : data.root;
  document.title = (q ? `“${q}” in ` : '') + folderName + ' · Media Browser';
  searchInput.placeholder = 'Search ' + folderName;
  renderCrumbs(data.root);
  if (q) {
    showEntries(data.results);
    showMessage(
      !data.results.length ? `Nothing named like “${q}” here.`
        : data.truncated ? `Showing the first ${data.results.length} matches.` : '');
  } else {
    showEntries(data.entries);
    showMessage(data.entries.length ? '' : 'This folder is empty. Upload something!');
  }
}

function showMessage(text) {
  message.textContent = text;
  message.hidden = !text;
}

function renderCrumbs(rootName) {
  crumbs.replaceChildren();
  const parts = current ? current.split('/') : [];
  const add = (label, path) => {
    if (crumbs.childNodes.length) crumbs.append(el('span', 'sep', '/'));
    const a = el('a');
    a.textContent = label;
    a.href = '#/' + enc(path);
    crumbs.append(a);
  };
  add(rootName, '');
  parts.forEach((p, i) => add(p, parts.slice(0, i + 1).join('/')));
  crumbs.scrollLeft = crumbs.scrollWidth;
}

/* ---------- Sorting ---------- */

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
const byName = (a, b) => collator.compare(a.name, b.name) || collator.compare(a.path || '', b.path || '');
const sorters = {
  name: byName,
  'name-desc': (a, b) => byName(b, a),
  newest: (a, b) => b.mtime - a.mtime || byName(a, b),
  oldest: (a, b) => a.mtime - b.mtime || byName(a, b),
  largest: (a, b) => b.size - a.size || byName(a, b),
  smallest: (a, b) => a.size - b.size || byName(a, b),
};

try {
  const saved = localStorage.getItem('sort');
  if (saved && sorters[saved]) sortSelect.value = saved;
} catch {}

let shown = [];
function showEntries(entries) {
  shown = entries;
  const cmp = sorters[sortSelect.value] || byName;
  // Folders always come first.
  renderGrid([...entries].sort((a, b) => (b.type === 'dir') - (a.type === 'dir') || cmp(a, b)));
}

sortSelect.addEventListener('change', () => {
  try {
    localStorage.setItem('sort', sortSelect.value);
  } catch {}
  showEntries(shown);
});

function renderGrid(entries) {
  viewable = entries.filter((e) => e.type === 'image' || e.type === 'video');
  const frag = document.createDocumentFragment();
  for (const e of entries) {
    // Search results carry a path relative to the current folder.
    const path = (e.full = join(current, e.path || e.name));
    const a = el('a', 'tile ' + e.type);
    a.title = e.name;
    const thumb = el('div', 'thumb', icons[e.type]);

    if (e.type === 'dir') {
      a.href = '#/' + enc(path);
      if (e.preview && e.preview.length) {
        thumb.replaceChildren();
        thumb.classList.add('collage', 'n' + e.preview.length);
        for (const p of e.preview) {
          const img = el('img');
          img.loading = 'lazy';
          img.decoding = 'async';
          img.alt = '';
          img.src = '/thumb/' + enc(join(path, p.path)) + '?v=' + p.mtime;
          img.onerror = () => (img.style.visibility = 'hidden');
          thumb.append(img);
        }
        thumb.append(el('span', 'badge', icons.dir));
      }
    } else {
      a.href = '/media/' + enc(path);
      if (e.type === 'other') {
        a.target = '_blank';
      } else {
        const idx = viewable.indexOf(e);
        a.addEventListener('click', (ev) => {
          if (ev.metaKey || ev.ctrlKey || ev.shiftKey) return;
          ev.preventDefault();
          openViewer(idx);
        });
        const img = el('img');
        img.loading = 'lazy';
        img.decoding = 'async';
        img.alt = '';
        img.src = '/thumb/' + enc(path) + '?v=' + e.mtime;
        img.onerror = () => img.remove();
        thumb.append(img);
        if (e.type === 'video') thumb.append(el('span', 'badge', icons.play));
      }
    }
    const name = el('div', 'name');
    name.textContent = e.name;
    a.append(thumb, name);
    if (e.path && e.path.includes('/')) {
      const where = el('div', 'where');
      where.textContent = e.path.slice(0, e.path.lastIndexOf('/'));
      a.title = e.path;
      a.append(where);
    }
    frag.append(a);
  }
  grid.replaceChildren(frag);
}

/* ---------- Lightbox ---------- */

function openViewer(i) {
  if (viewer.hidden) {
    history.pushState({ viewer: true }, '');
    viewer.hidden = false;
    document.body.classList.add('noscroll');
  }
  viewIndex = i;
  showCurrent();
}

function closeViewer(fromHistory) {
  if (viewer.hidden) return;
  viewer.hidden = true;
  stage.replaceChildren(); // stops video playback
  document.body.classList.remove('noscroll');
  if (!fromHistory && history.state && history.state.viewer) history.back();
}

function showCurrent() {
  const e = viewable[viewIndex];
  const src = '/media/' + enc(e.full);
  let media;
  if (e.type === 'image') {
    media = el('img');
    media.alt = e.name;
  } else {
    media = el('video');
    media.controls = true;
    media.autoplay = true;
    media.playsInline = true;
  }
  media.src = src;
  stage.replaceChildren(media);
  $('#v-caption').textContent = `${e.name}  ·  ${viewIndex + 1} / ${viewable.length}`;
  $('#v-download').href = src + '?download';
  $('#v-prev').disabled = viewIndex === 0;
  $('#v-next').disabled = viewIndex === viewable.length - 1;
  // Preload the next image so stepping through feels instant.
  const next = viewable[viewIndex + 1];
  if (next && next.type === 'image') new Image().src = '/media/' + enc(next.full);
}

function step(delta) {
  const i = viewIndex + delta;
  if (i >= 0 && i < viewable.length) {
    viewIndex = i;
    showCurrent();
  }
}

$('#v-prev').onclick = () => step(-1);
$('#v-next').onclick = () => step(1);
$('#v-close').onclick = () => closeViewer(false);
stage.addEventListener('click', (ev) => {
  if (ev.target === stage) closeViewer(false);
});

document.addEventListener('keydown', (ev) => {
  if (viewer.hidden) return;
  if (ev.key === 'Escape') closeViewer(false);
  else if (ev.key === 'ArrowLeft') step(-1);
  else if (ev.key === 'ArrowRight') step(1);
});

let touchX = null;
let touchY = null;
viewer.addEventListener('touchstart', (ev) => {
  if (ev.touches.length !== 1) return (touchX = null);
  touchX = ev.touches[0].clientX;
  touchY = ev.touches[0].clientY;
}, { passive: true });
viewer.addEventListener('touchend', (ev) => {
  if (touchX === null) return;
  const dx = ev.changedTouches[0].clientX - touchX;
  const dy = ev.changedTouches[0].clientY - touchY;
  touchX = null;
  if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.5) step(dx < 0 ? 1 : -1);
  else if (dy > 100 && Math.abs(dy) > Math.abs(dx) * 1.5) closeViewer(false);
});

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
  if (ev.key === '/' && viewer.hidden && document.activeElement !== searchInput) {
    ev.preventDefault();
    searchInput.focus();
  }
});

/* ---------- Uploads ---------- */

let activeUploads = 0;

function formatBytes(n) {
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return (i ? n.toFixed(1) : n) + ' ' + units[i];
}

async function uploadFiles(files) {
  files = files.filter((f) => f.size > 0 || f.type);
  if (!files.length) return;
  const dest = current;
  uploadsBox.hidden = false;
  const rows = files.map((f) => {
    const row = el('div', 'up');
    const n = el('span', 'n');
    n.textContent = f.name;
    const s = el('span', 's');
    s.textContent = 'Waiting · ' + formatBytes(f.size);
    const bar = el('div', 'bar', '<div></div>');
    row.append(n, s, bar);
    uploadsBox.append(row);
    return { file: f, row, s, fill: bar.firstChild };
  });

  activeUploads++;
  let ok = 0;
  for (const r of rows) {
    if (await uploadOne(r, dest)) ok++;
  }
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
    fd.append('file', r.file, r.file.name);
    r.s.textContent = '0%';
    xhr.send(fd);
  });
}

$('#file-input').addEventListener('change', (ev) => {
  uploadFiles([...ev.target.files]);
  ev.target.value = '';
});

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
  uploadFiles([...ev.dataTransfer.files]);
});

load();
