// Script for settings.html, moved out of the page so the Content-Security-Policy
// can refuse inline scripts. Order relative to the page's other scripts is
// unchanged.
const form = document.getElementById('form');
const statusEl = document.getElementById('status');
let META = [];

function inputFor(f, value) {
  const wrap = document.createElement('label');
  wrap.className = 'field';

  const head = document.createElement('span');
  head.className = 'field-head';
  head.textContent = f.label;
  if (f.restart_required) {
    const b = document.createElement('span');
    b.className = 'badge warn';
    b.textContent = 'restart';
    head.appendChild(b);
  }
  if (f.read_only) {
    const b = document.createElement('span');
    b.className = 'badge';
    b.textContent = 'locked';
    head.appendChild(b);
  }
  wrap.appendChild(head);

  const input = document.createElement('input');
  input.dataset.path = f.path;
  input.value = value == null ? '' : value;
  if (f.kind === 'bool') { input.type = 'checkbox'; input.checked = value === true; input.style.width = '22px'; input.style.height = '22px'; input.style.margin = '4px 0'; input.style.alignSelf = 'flex-start'; }
  else if (f.kind === 'int' || f.kind === 'float') input.type = 'number';
  else if (f.secret) input.type = 'password';
  else input.type = 'text';
  if (f.read_only) input.disabled = true;
  wrap.appendChild(input);

  if (f.secret && !f.read_only) {
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'reveal';
    toggle.textContent = 'show';
    toggle.onclick = () => {
      const showing = input.type === 'text';
      input.type = showing ? 'password' : 'text';
      toggle.textContent = showing ? 'show' : 'hide';
    };
    wrap.appendChild(toggle);
  }
  if (f.help) {
    const h = document.createElement('span');
    h.className = 'muted small';
    h.textContent = f.help;
    wrap.appendChild(h);
  }
  return wrap;
}

function render(fields, values) {
  META = fields;
  form.innerHTML = '';
  const sections = [];
  const bySection = {};
  for (const f of fields) {
    if (!bySection[f.section]) { bySection[f.section] = []; sections.push(f.section); }
    bySection[f.section].push(f);
  }
  for (const name of sections) {
    const sec = document.createElement('section');
    sec.className = 'section';
    const h = document.createElement('h2');
    h.textContent = name;
    sec.appendChild(h);
    const grid = document.createElement('div');
    grid.className = 'grid';
    for (const f of bySection[name]) grid.appendChild(inputFor(f, values[f.path]));
    sec.appendChild(grid);
    form.appendChild(sec);
  }
}

async function load() {
  statusEl.textContent = 'Loading…';
  const res = await fetch('/api/settings');
  const data = await res.json();
  render(data.fields, data.values);
  statusEl.textContent = '';
}

async function save() {
  const edits = {};
  for (const f of META) {
    if (f.read_only) continue;
    const el = form.querySelector('input[data-path="' + f.path + '"]');
    if (el) edits[f.path] = el.type === 'checkbox' ? el.checked : el.value;
  }
  statusEl.textContent = 'Saving…';
  const res = await fetch('/api/admin/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(edits)
  });
  const data = await res.json();
  if (!data.ok) { statusEl.innerHTML = '<span class="err">' + (data.error || 'Save failed') + '</span>'; return; }
  if (data.restart_required && data.restart_required.length) {
    statusEl.innerHTML = '✓ Saved. <span class="err">Restart required for: ' + data.restart_required.join(', ') + '</span>';
  } else {
    statusEl.textContent = '✓ Saved and applied live.';
  }
}

document.getElementById('save').addEventListener('click', save);
load();
