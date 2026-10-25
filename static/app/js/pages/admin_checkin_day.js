// Script for admin_checkin_day.html, moved out of the page so the Content-Security-Policy
// can refuse inline scripts. Order relative to the page's other scripts is
// unchanged.
const DATE = decodeURIComponent((location.pathname.split('/day/')[1] || '').replace(/\/+$/, ''));
let ROWS = [];
let MANUAL = 0;   // people served on hand-written tickets, entered below
let sortKey = 'time', sortAsc = true;
function esc(v) { return v == null ? '' : String(v).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;'); }

document.getElementById('title').textContent = '📅 ' + DATE;
document.getElementById('dl').href = '/api/visits/days/' + encodeURIComponent(DATE) + '/export.xlsx';

async function load() {
  document.getElementById('content').innerHTML = 'Loading…';
  const data = await (await fetch('/api/visits/days/' + encodeURIComponent(DATE))).json();
  ROWS = data.result || [];
  MANUAL = (data.manual || {}).shopped_for || 0;
  render();
}

function dupIds(rows) {
  const seen = {}, dups = new Set();
  rows.forEach(ci => { if (seen[ci.uuid]) dups.add(ci.ULID); else seen[ci.uuid] = true; });
  return dups;
}

function sortRows(rows) {
  return rows.slice().sort((a, b) => {
    let x, y;
    if (sortKey === 'name') { x = (a.name || '').toLowerCase(); y = (b.name || '').toLowerCase(); }
    else if (sortKey === 'for') { x = a.shopping.shopping_for; y = b.shopping.shopping_for; }
    else { x = a.time; y = b.time; }
    return (x < y ? -1 : x > y ? 1 : 0) * (sortAsc ? 1 : -1);
  });
}

function render() {
  const dups = dupIds(ROWS);
  const shopped = ROWS.reduce((s, ci) => s + (ci.shopping.shopping_for || 0), 0);
  const fresh = ROWS.filter(ci => ci.first_visit).length;
  const pills = document.getElementById('pills');
  // The headline figure is what actually happened that day: app check-ins plus
  // whoever was served on paper.
  pills.innerHTML = '<span class="pill">' + ROWS.length + ' check-ins</span>' +
    '<span class="pill b">' + (shopped + MANUAL) + ' people shopped for</span>' +
    (MANUAL ? '<span class="pill m">' + shopped + ' here + ' + MANUAL + ' manual</span>' : '') +
    (fresh ? '<span class="pill new">🆕 ' + fresh + ' new ' + (fresh === 1 ? 'family' : 'families') + '</span>' : '') +
    (dups.size ? '<span class="pill dup">' + dups.size + ' duplicate' + (dups.size > 1 ? 's' : '') + '</span>' : '');
  if (dups.size) {
    const btn = document.createElement('button');
    btn.className = 'btn danger small'; btn.textContent = 'Delete duplicates';
    btn.onclick = deleteDuplicates; pills.appendChild(btn);
  }
  pills.appendChild(manualEditor());

  const c = document.getElementById('content');
  if (!ROWS.length) { c.innerHTML = '<p class="muted">No check-ins on this day.</p>'; return; }

  const cols = [['name', 'Name'], ['time', 'Time'], ['type', 'Type'], ['for', 'For'],
    ['tops', 'Tp'], ['bottoms', 'Bt'], ['dresses', 'Dr'], ['shoes', 'Sh'], ['seasonals', 'Se'], ['accessories', 'Ac'], ['guests', 'Gu']];
  let html = '<div class="scroll"><table><thead><tr>';
  cols.forEach(col => {
    const sortable = (col[0] === 'name' || col[0] === 'time' || col[0] === 'for');
    html += '<th data-k="' + col[0] + '"' + (sortable ? ' style="cursor:pointer"' : '') + '>' + col[1] +
      (sortable && sortKey === col[0] ? (sortAsc ? ' ▲' : ' ▼') : '') + '</th>';
  });
  html += '<th></th></tr></thead><tbody>';
  sortRows(ROWS).forEach(ci => {
    const s = ci.shopping || {};
    const cls = [dups.has(ci.ULID) ? 'dup' : '', ci.first_visit ? 'new' : ''].filter(Boolean).join(' ');
    html += '<tr class="' + cls + '">' +
      '<td>' + esc(ci.name) + (ci.first_visit ? ' <span class="badge-new" title="First visit">🆕 New</span>' : '') + '</td><td>' + esc(ci.time) + '</td><td>' + esc(ci.type) + '</td>' +
      '<td class="num">' + (s.shopping_for || 0) + '</td>' +
      ['tops', 'bottoms', 'dresses', 'shoes', 'seasonals', 'accessories', 'guests'].map(k => '<td class="num">' + (s[k] || 0) + '</td>').join('') +
      '<td><button class="del" data-uuid="' + ci.uuid + '" data-ulid="' + ci.ULID + '" title="Delete">🗑</button></td></tr>';
  });
  html += '</tbody></table></div>';
  c.innerHTML = html;
  c.querySelectorAll('th[data-k]').forEach(th => {
    const k = th.dataset.k;
    if (k === 'name' || k === 'time' || k === 'for') th.onclick = () => { if (sortKey === k) sortAsc = !sortAsc; else { sortKey = k; sortAsc = true; } render(); };
  });
  c.querySelectorAll('.del').forEach(b => b.onclick = () => deleteOne(b.dataset.uuid, b.dataset.ulid));
}

// Manual tally: people served with hand-written tickets that never went through
// the app. Stored per day (not per user), so the totals can be made truthful
// after the fact.
function manualEditor() {
  const wrap = document.createElement('label');
  wrap.className = 'manual';
  wrap.appendChild(document.createTextNode('🖊 Manual people:'));
  const input = document.createElement('input');
  input.type = 'number'; input.min = '0'; input.value = MANUAL;
  const save = document.createElement('button');
  save.className = 'btn ghost small'; save.type = 'button'; save.textContent = 'Save';
  const msg = document.createElement('span');
  msg.className = 'muted small';
  save.onclick = async () => {
    const n = parseInt(input.value || '0', 10) || 0;
    msg.textContent = 'Saving…';
    const res = await fetch('/api/visits/days/' + encodeURIComponent(DATE) + '/manual', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ shopped_for: n })
    });
    const data = await res.json();
    if (!res.ok || !data.result) { msg.innerHTML = '<span class="err">' + esc(data.error || 'Error') + '</span>'; return; }
    msg.textContent = '';
    MANUAL = n;
    render();
  };
  wrap.appendChild(input); wrap.appendChild(save); wrap.appendChild(msg);
  return wrap;
}

async function deleteOne(uuid, ulid) {
  if (!confirm('Delete this check-in?')) return;
  await fetch('/api/admin/visits/' + encodeURIComponent(ulid) + '/delete', { method: 'POST' });
  await load();
}

async function deleteDuplicates() {
  const dups = dupIds(ROWS);
  const targets = ROWS.filter(ci => dups.has(ci.ULID));
  if (!confirm('Delete ' + targets.length + ' duplicate check-in(s)? (keeps the first per person)')) return;
  for (const ci of targets) await fetch('/api/admin/visits/' + encodeURIComponent(ci.ULID) + '/delete', { method: 'POST' });
  await load();
}

load();
