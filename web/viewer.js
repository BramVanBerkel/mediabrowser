// Fullscreen lightbox for images and videos, with zoom and pan for images.
import { $, el, enc, formatBytes } from './util.js';

const viewer = $('#viewer');
const stage = $('#v-stage');
const plane = $('#v-plane');
const zoomLevel = $('#v-zoom-level');
const zoomOutBtn = $('#v-zoom-out');
const zoomInBtn = $('#v-zoom-in');
const zoomResetBtn = $('#v-zoom-reset');

const MAX_SCALE = 8;
const STEP = Math.exp(0.4); // zoom factor of the buttons and + / - keys

let items = [];  // images and videos in display order
let index = -1;
let media = null; // the <img> or <video> on stage
let scale = 1;
let tx = 0;      // translation of the plane, in px
let ty = 0;
let returnFocus = null;

export const viewerOpen = () => !viewer.hidden;

export function openViewer(list, i) {
  items = list;
  if (viewer.hidden) {
    returnFocus = document.activeElement;
    history.pushState({ viewer: true }, '');
    viewer.hidden = false;
    document.body.classList.add('noscroll');
    viewer.focus();
  }
  show(i);
}

export function closeViewer(fromHistory) {
  if (viewer.hidden) return;
  viewer.hidden = true;
  plane.replaceChildren(); // stops video playback
  media = null;
  document.body.classList.remove('noscroll');
  if (returnFocus && returnFocus.isConnected) returnFocus.focus();
  if (!fromHistory && history.state && history.state.viewer) history.back();
}

function show(i) {
  index = i;
  const e = items[i];
  const src = '/media/' + enc(e.full);
  if (e.type === 'image') {
    media = el('img');
    media.alt = e.name;
    media.draggable = false;
  } else {
    media = el('video');
    media.controls = true;
    media.autoplay = true;
    media.playsInline = true;
  }
  media.src = src;
  plane.replaceChildren(media);
  setTransform(1, 0, 0);

  $('#v-title').textContent = e.name;
  $('#v-count').textContent = `${i + 1} / ${items.length}`;
  $('#v-download').href = src + '?download';
  $('#v-kind').textContent = e.type === 'video' ? 'Video' : 'Image';
  $('#v-size').textContent = formatBytes(e.size);
  $('#v-prev').hidden = i === 0;
  $('#v-next').hidden = i === items.length - 1;
  $('#v-zoom').hidden = e.type !== 'image';
  // Preload the next image so stepping through feels instant.
  const next = items[i + 1];
  if (next && next.type === 'image') new Image().src = '/media/' + enc(next.full);
}

function step(delta) {
  const i = index + delta;
  if (i >= 0 && i < items.length) show(i);
}

/* ---------- Zoom and pan ---------- */

const zoomable = () => media && media.tagName === 'IMG';

// The stage size and the size of the image fitted inside it at scale 1.
function fitBox() {
  const W = stage.clientWidth;
  const H = stage.clientHeight;
  const nw = (media && media.naturalWidth) || W;
  const nh = (media && media.naturalHeight) || H;
  const f = Math.min(W / nw, H / nh);
  return { W, H, w: nw * f, h: nh * f };
}

// Clamps a translation so the zoomed image covers the stage along one axis,
// or is centred when it is smaller than the stage.
function clampAxis(t, s, size, img) {
  const scaled = img * s;
  if (scaled <= size) return (size - size * s) / 2;
  const off = ((size - img) / 2) * s; // where the image starts inside the scaled plane
  return Math.min(-off, Math.max(size - off - scaled, t));
}

function setTransform(s, x, y, animate) {
  s = Math.min(MAX_SCALE, Math.max(1, s));
  const b = fitBox();
  scale = s;
  tx = clampAxis(x, s, b.W, b.w);
  ty = clampAxis(y, s, b.H, b.h);
  plane.classList.toggle('animate', !!animate);
  // No transform at fit scale, so videos (never zoomed) are not in a transformed layer.
  plane.style.transform = s === 1 ? '' : `translate(${tx}px, ${ty}px) scale(${s})`;
  stage.classList.toggle('zoomed', s > 1.01);
  zoomLevel.textContent = Math.round(s * 100) + '%';
  zoomOutBtn.disabled = zoomResetBtn.disabled = s <= 1.01;
  zoomInBtn.disabled = s >= MAX_SCALE - 0.01;
}

// Zooms to s, keeping the point (px, py) of the stage in place.
function zoomAt(s, px, py, animate) {
  s = Math.min(MAX_SCALE, Math.max(1, s));
  setTransform(s, px - ((px - tx) / scale) * s, py - ((py - ty) / scale) * s, animate);
}

function zoomBy(factor) {
  if (zoomable()) zoomAt(scale * factor, stage.clientWidth / 2, stage.clientHeight / 2, true);
}

function resetZoom() {
  if (zoomable()) setTransform(1, 0, 0, true);
}

zoomInBtn.onclick = () => zoomBy(STEP);
zoomOutBtn.onclick = () => zoomBy(1 / STEP);
zoomResetBtn.onclick = resetZoom;

const local = (ev) => {
  const r = stage.getBoundingClientRect();
  return { x: ev.clientX - r.left, y: ev.clientY - r.top };
};

stage.addEventListener('wheel', (ev) => {
  if (!zoomable()) return;
  ev.preventDefault();
  // Trackpad pinches arrive as ctrl+wheel with small deltas.
  const k = ev.ctrlKey ? 0.01 : 0.0015;
  const dy = ev.deltaMode === 1 ? ev.deltaY * 16 : ev.deltaY;
  const p = local(ev);
  zoomAt(scale * Math.exp(-dy * k), p.x, p.y);
}, { passive: false });

stage.addEventListener('dblclick', (ev) => {
  if (!zoomable() || ev.target.closest('button')) return;
  const p = local(ev);
  if (scale > 1.01) resetZoom();
  else zoomAt(2, p.x, p.y, true);
});

// Pointer gestures: drag pans a zoomed image, two fingers pinch, and at fit
// scale a touch swipe steps (left/right) or closes (down).
const pointers = new Map();
let gesture = null;
let pinched = false; // a pinch happened since the first finger went down
let moved = false;   // the pointer moved, so the click that follows is not a tap

function startGesture() {
  const pts = [...pointers.values()];
  if (pts.length === 2 && zoomable()) {
    const [a, b] = pts;
    const r = stage.getBoundingClientRect();
    pinched = true;
    gesture = {
      type: 'pinch',
      d: Math.hypot(a.x - b.x, a.y - b.y) || 1,
      mx: (a.x + b.x) / 2 - r.left,
      my: (a.y + b.y) / 2 - r.top,
      s: scale, tx, ty,
    };
  } else if (pts.length === 1) {
    gesture = { type: scale > 1.01 ? 'pan' : 'swipe', x: pts[0].x, y: pts[0].y, tx, ty };
  } else {
    gesture = null;
  }
}

stage.addEventListener('pointerdown', (ev) => {
  if (!media || ev.target.closest('button') || (ev.pointerType === 'mouse' && ev.button !== 0)) return;
  if (!pointers.size) {
    pinched = false;
    moved = false;
  }
  pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY });
  if (zoomable()) stage.setPointerCapture(ev.pointerId); // leave video controls alone
  startGesture();
});

stage.addEventListener('pointermove', (ev) => {
  if (!pointers.has(ev.pointerId)) return;
  pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY });
  if (!gesture) return;
  if (gesture.type === 'pinch' && pointers.size === 2) {
    const [a, b] = [...pointers.values()];
    const r = stage.getBoundingClientRect();
    const s = Math.min(MAX_SCALE, Math.max(1, (gesture.s * Math.hypot(a.x - b.x, a.y - b.y)) / gesture.d));
    // Keep the point that was under the fingers under their current midpoint.
    const u = (gesture.mx - gesture.tx) / gesture.s;
    const v = (gesture.my - gesture.ty) / gesture.s;
    setTransform(s, (a.x + b.x) / 2 - r.left - u * s, (a.y + b.y) / 2 - r.top - v * s);
    moved = true;
    return;
  }
  const dx = ev.clientX - gesture.x;
  const dy = ev.clientY - gesture.y;
  if (Math.abs(dx) + Math.abs(dy) > 6) moved = true;
  if (gesture.type === 'pan') {
    stage.classList.add('dragging');
    setTransform(scale, gesture.tx + dx, gesture.ty + dy);
  }
});

function endPointer(ev) {
  if (!pointers.has(ev.pointerId)) return;
  pointers.delete(ev.pointerId);
  stage.classList.remove('dragging');
  if (gesture && gesture.type === 'swipe' && !pinched && ev.type === 'pointerup' && ev.pointerType !== 'mouse') {
    const dx = ev.clientX - gesture.x;
    const dy = ev.clientY - gesture.y;
    if (Math.abs(dx) > 48 && Math.abs(dx) > Math.abs(dy) * 1.5) step(dx < 0 ? 1 : -1);
    else if (dy > 100 && Math.abs(dy) > Math.abs(dx) * 1.5) closeViewer(false);
  }
  startGesture(); // e.g. continue panning with the finger left after a pinch
}
stage.addEventListener('pointerup', endPointer);
stage.addEventListener('pointercancel', endPointer);

// A tap or click beside the image closes the lightbox.
stage.addEventListener('click', (ev) => {
  if (moved || scale > 1.01 || !media || ev.target.closest('button') || ev.target.tagName === 'VIDEO') return;
  if (zoomable()) {
    const b = fitBox();
    const p = local(ev);
    if (Math.abs(p.x - b.W / 2) <= b.w / 2 && Math.abs(p.y - b.H / 2) <= b.h / 2) return;
  }
  closeViewer(false);
});

window.addEventListener('resize', () => {
  if (media) setTransform(scale, tx, ty);
});

/* ---------- Buttons and keys ---------- */

$('#v-prev').onclick = () => step(-1);
$('#v-next').onclick = () => step(1);
$('#v-close').onclick = () => closeViewer(false);

document.addEventListener('keydown', (ev) => {
  if (viewer.hidden || ev.ctrlKey || ev.metaKey || ev.altKey) return;
  switch (ev.key) {
    case 'Escape': closeViewer(false); break;
    case 'ArrowLeft': step(-1); break;
    case 'ArrowRight': step(1); break;
    case '+': case '=': zoomBy(STEP); break;
    case '-': zoomBy(1 / STEP); break;
    case '0': resetZoom(); break;
    default: return;
  }
  ev.preventDefault();
});
