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

const dateFormat = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
export const formatDate = (ms) => dateFormat.format(new Date(ms));
