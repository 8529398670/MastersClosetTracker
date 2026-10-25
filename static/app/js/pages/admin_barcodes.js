// Script for admin_barcodes.html: every barcode on every family, with the
// duplicates (one barcode on several families) flagged.
let ROWS = [];
function esc(v) { return v == null ? '' : String(v).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;'); }

// Go sends a zero time for "no date known".
function dated(r) { return r.assigned_at && !r.assigned_at.startsWith('0001-') ? new Date(r.assigned_at) : null; }

function when(r) {
  const d = dated(r);
  if (!d) return '<span class="when est">date unknown</span>';
  const text = d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  return r.estimated
    ? '<span class="when est" title="Estimated">~' + esc(text) + '</span>'
    : '<span class="when">' + esc(text) + '</span>';
}

function stat(n, label, cls) {
  return '<div class="stat ' + (cls || '') + '"><div class="num">' + n.toLocaleString() + '</div><div class="lbl">' + label + '</div></div>';
}

async function load() {
  document.getElementById('list').innerHTML = 'Loading…';
  ROWS = (await (await fetch('/api/clients/barcodes')).json()).result || [];
  render();
}

function byDate(a, b) {
  const x = dated(a), y = dated(b);
  if (!x && !y) return a.barcode.localeCompare(b.barcode);
  if (!x) return 1;
  if (!y) return -1;
  return x - y;
}

function render() {
  const order = document.getElementById('order').value;
  const dupesOnly = document.getElementById('dupes').checked;
  const q = document.getElementById('q').value.trim().toLowerCase();

  const families = new Set(ROWS.map(r => r.client_id)).size;
  const dupes = new Set(ROWS.filter(r => r.owners > 1).map(r => r.barcode)).size;
  document.getElementById('banner').innerHTML =
    stat(ROWS.length, 'Barcodes') + stat(families, 'Families') + stat(dupes, 'Duplicate Barcodes', dupes ? 'warn' : '');

  let rows = ROWS.filter(r => (!dupesOnly || r.owners > 1) &&
    (!q || r.barcode.toLowerCase().includes(q) || (r.name || '').toLowerCase().includes(q)));
  if (order === 'oldest') rows.sort(byDate);
  else if (order === 'barcode') rows.sort((a, b) => a.barcode.localeCompare(b.barcode) || (a.name || '').localeCompare(b.name || ''));
  else if (order === 'name') rows.sort((a, b) => (a.name || '').localeCompare(b.name || '') || a.barcode.localeCompare(b.barcode));
  // else: the backend's order, newest first.
  // Keep a duplicate's families together so they can be compared.
  if (dupesOnly && order !== 'barcode') rows.sort((a, b) => a.barcode.localeCompare(b.barcode));

  const el = document.getElementById('list');
  if (!rows.length) { el.innerHTML = '<p class="muted">' + (ROWS.length ? 'No matching barcodes.' : 'No barcodes yet.') + '</p>'; return; }
  el.innerHTML = rows.map(r =>
    '<div class="list-row bc-item' + (r.owners > 1 ? ' dupe' : '') + '">' +
      '<span class="bc-main"><span class="code">' + esc(r.barcode) + '</span><span class="who">' + esc(r.name || '(no name)') + '</span></span>' +
      '<span class="bc-side">' +
        (r.owners > 1 ? '<span class="pill">⚠ shared ×' + r.owners + '</span>' : '') +
        when(r) +
        '<a class="btn ghost small" href="/admin/users/' + encodeURIComponent(r.client_id) + '">Edit</a>' +
      '</span>' +
    '</div>').join('');
}

document.getElementById('order').addEventListener('change', render);
document.getElementById('dupes').addEventListener('change', render);
document.getElementById('q').addEventListener('input', render);
load();
