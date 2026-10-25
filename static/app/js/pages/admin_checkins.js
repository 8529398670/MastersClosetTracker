// Script for admin_checkins.html, moved out of the page so the Content-Security-Policy
// can refuse inline scripts. Order relative to the page's other scripts is
// unchanged.
let DAYS = [];
function esc(v) { return v == null ? '' : String(v).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;'); }

async function load() {
  document.getElementById('list').innerHTML = 'Loading…';
  DAYS = (await (await fetch('/api/visits/days')).json()).result || [];
  render();
}

// People actually served that day: app check-ins plus the manual tally entered
// on the day page for hand-written tickets.
function served(d) { return (d.shopped_for || 0) + (d.manual_shopped_for || 0); }

function stat(n, label) {
  return '<div class="stat"><div class="num">' + n.toLocaleString() + '</div><div class="lbl">' + label + '</div></div>';
}

function render() {
  const order = document.getElementById('order').value;
  let days = DAYS.slice();              // backend gives newest-first
  if (order === 'asc') days.reverse();
  else if (order === 'busy') days.sort((a, b) => b.count - a.count);
  else if (order === 'shopped') days.sort((a, b) => served(b) - served(a));

  const tc = DAYS.reduce((s, d) => s + d.count, 0);
  const ts = DAYS.reduce((s, d) => s + served(d), 0);
  document.getElementById('banner').innerHTML =
    stat(DAYS.length, 'Collection Days') + stat(tc, 'Total Check-Ins') + stat(ts, 'People Shopped For');

  const el = document.getElementById('list');
  if (!days.length) { el.innerHTML = '<p class="muted">No check-ins yet.</p>'; return; }
  el.innerHTML = '';
  days.forEach(d => {
    const a = document.createElement('a');
    a.className = 'list-row day-link';
    a.href = '/admin/checkins/day/' + encodeURIComponent(d.date);
    a.target = '_blank';
    a.rel = 'noopener';
    a.innerHTML = '<span class="date">' + esc(d.date) + '</span>' +
      '<span class="row" style="gap:6px">' +
        '<span class="pill">' + d.count + ' check-ins</span>' +
        '<span class="pill b">' + served(d) + ' shopped for</span>' +
        (d.manual_shopped_for ? '<span class="pill m">🖊 ' + d.manual_shopped_for + ' manual</span>' : '') +
      '</span>';
    el.appendChild(a);
  });
}

document.getElementById('order').addEventListener('change', render);
load();
