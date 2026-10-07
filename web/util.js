export const $ = (sel) => document.querySelector(sel);

export const enc = (p) => p.split('/').map(encodeURIComponent).join('/');
export const join = (dir, name) => (dir ? dir + '/' + name : name);

export const el = (tag, cls, html) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (html) e.innerHTML = html;
  return e;
};

export function formatBytes(n) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return (i ? n.toFixed(1) : n) + ' ' + units[i];
}

// Reduces a waveform's peaks (0 to 100) to n bars, each the loudest of the
// peaks it covers. With fewer peaks than bars, peaks span several bars.
export function barPeaks(peaks, n) {
  const bars = [];
  for (let i = 0; i < n; i++) {
    const from = Math.floor((i * peaks.length) / n);
    const to = Math.max(from + 1, Math.floor(((i + 1) * peaks.length) / n));
    let v = 0;
    for (let j = from; j < to; j++) v = Math.max(v, peaks[j]);
    bars.push(v);
  }
  return bars;
}

const dateFormat = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
export const formatDate = (ms) => dateFormat.format(new Date(ms));
