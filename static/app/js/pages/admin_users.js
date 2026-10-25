// Script for admin_users.html, moved out of the page so the Content-Security-Policy
// can refuse inline scripts. Order relative to the page's other scripts is
// unchanged.
const listEl = document.getElementById('list');
const editorEl = document.getElementById('editor');
let current = null;     // full loaded user
let LANGS = [{ code: 'en', name: 'English' }];
let MAXFAM = 0;   // largest family incl. the account holder; 0 = no limit

// True when we arrived from the check-in page (its Edit button). Save and Cancel
// then return there with the same user pulled up, instead of just closing.
const cameFromCheckin = /\/admin\/checkin(\/|\?|#|$)/.test(document.referrer || '');

// Deep-linked to one user (/admin/users/<uuid>, e.g. the check-in page's Edit
// button): the page is an editor, not a browser. The list stays out of the way —
// it sits above the editor, so loading it pushed the form off the screen — until
// a search or Cancel asks for it.
let editorOnly = false;
function showList(on) {
  listEl.style.display = on ? '' : 'none';
  if (!on) listEl.innerHTML = '';
}

function esc(v) { return v == null ? '' : String(v).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;'); }
function debounce(fn, ms) { let t; return function () { clearTimeout(t); const a = arguments; t = setTimeout(() => fn.apply(null, a), ms); }; }

function row(u) {
  const div = document.createElement('div');
  div.className = 'list-row';
  // Recent rows carry the time too; trim it to HH:MM.
  const when = u.last_check_in_date
    ? u.last_check_in_date + (u.last_check_in_time ? ' ' + u.last_check_in_time.slice(0, 5) : '')
    : 'never';
  div.innerHTML = '<strong>' + esc(u.name_string || u.username || '(no name)') + '</strong>' +
    '<span class="muted small">' + esc(when) + '</span>';
  div.onclick = () => pickUser(u.uuid);
  return div;
}

// A pick from the list clears the search and puts the page in editor-only
// mode, as a deep link does, so the form sits at the top. Typing a new
// search or Cancel brings the list back.
function pickUser(uuid) {
  document.getElementById('q').value = '';
  editorOnly = true;
  showList(false);
  loadUser(uuid);
}

async function search() {
  const v = document.getElementById('q').value.trim();
  if (v === '') { loadRecent(); return; }
  // Searching is an explicit ask for the list, even on a deep link.
  if (editorOnly) { editorOnly = false; showList(true); }
  if (v.length < 2) { listEl.innerHTML = '<p class="muted small">Type at least 2 letters.</p>'; return; }
  listEl.innerHTML = 'Searching…';
  const data = await (await fetch('/api/clients/search?q=' + encodeURIComponent(v))).json();
  renderList(data.result || []);
}
const liveSearch = debounce(search, 250);

// The default list under an empty search box: who came in most recently.
async function loadRecent() {
  if (editorOnly) return; // deep-linked editor: nothing above the form
  let users = [];
  try { users = (await (await fetch('/api/clients/recent?limit=25')).json()).result || []; } catch (e) {}
  if (document.getElementById('q').value.trim() !== '') return; // the user started typing meanwhile
  listEl.innerHTML = '';
  if (!users.length) return;
  const head = document.createElement('p');
  head.className = 'muted small';
  head.style.margin = '0';
  head.textContent = 'Recently checked in';
  listEl.appendChild(head);
  users.forEach(u => listEl.appendChild(row(u)));
}

function renderList(users) {
  listEl.innerHTML = '';
  if (!users.length) { listEl.innerHTML = '<p class="muted">No users found.</p>'; return; }
  users.forEach(u => listEl.appendChild(row(u)));
}

async function loadUser(uuid) {
  const data = await (await fetch('/api/clients/' + encodeURIComponent(uuid))).json();
  current = data.result || data;
  history.replaceState(null, '', '/admin/users/' + uuid); // keep URL navigable/back-able
  renderEditor(current);
  editorEl.scrollIntoView({ behavior: 'smooth' });
}

// lk is the field_labels.js key for the label's translation.
function field(label, value, key, lk) {
  return '<label class="field"><span class="field-head">' + label + fieldTr(lk) + '</span>' +
    '<input data-key="' + key + '" value="' + esc(value) + '"></label>';
}

function langOptions(selected) {
  return LANGS.map(l => '<option value="' + l.code + '"' + (l.code === selected ? ' selected' : '') + '>' + esc(l.name) + '</option>').join('');
}

function renderEditor(u) {
  const id = u.identity || {}, ad = id.address || {};
  const lang = u.language || (u.spanish ? 'es' : 'en');
  editorEl.innerHTML =
    '<section class="section"><div class="row between"><h2 style="margin:0">Edit — ' + esc(u.name_string || '') + '</h2>' +
      '<span class="row nowrap" style="gap:14px"><span id="verifiedSlot"></span><button class="btn ghost small" id="cancelBtn">✕ Cancel</button></span></div>' +
    '<div class="grid">' +
      field('First name', id.first_name, 'identity.first_name', 'first_name') +
      field('Middle name', id.middle_name, 'identity.middle_name', 'middle_name') +
      field('Last name', id.last_name, 'identity.last_name', 'last_name') +
      field('Email', u.email_address, 'email_address', 'email') +
      field('Phone', u.phone_number, 'phone_number', 'phone') +
      '<label class="field"><span class="field-head">Family size' + fieldTr('family_size') + '<span class="muted small" style="font-weight:400">(calculated)</span></span>' +
        '<input data-key="family_size" value="' + esc(u.family_size) + '" readonly tabindex="-1" ' +
        'title="Calculated from the family members below (self + members)" ' +
        'style="background:var(--bg);color:var(--muted);cursor:not-allowed"></label>' +
      field('Street #', ad.street_number, 'identity.address.street_number', 'street_number') +
      field('Street name', ad.street_name, 'identity.address.street_name', 'street_name') +
      field('City', ad.city, 'identity.address.city', 'city') +
      field('State', ad.state, 'identity.address.state', 'state') +
      field('Zip', ad.zipcode, 'identity.address.zipcode', 'zip') +
      '<label class="field"><span class="field-head">Ticket language' + fieldTr('language') + '</span>' +
        '<select id="langSel">' + langOptions(lang) + '</select></label>' +
    '</div>' +
    // Family members and barcodes side by side on a wide screen (stacked on a
    // phone): a barcode is short, so a full-width box wasted the space.
    '<div class="fm-bc">' +
    '<div class="field"><span class="field-head">Family members' + fieldTr('family_members') + '</span>' +
      '<div id="fmList"></div>' +
      '<button class="btn ghost small" id="addFmBtn" type="button" style="align-self:flex-start">＋ Add family member</button>' +
      '<span id="fmNote" class="muted small"></span>' +
    '</div>' +
    '<div class="field"><span class="field-head">Barcodes' + fieldTr('barcodes') + '</span>' +
      '<div id="bcList"></div>' +
      '<button class="btn ghost small" id="addBcBtn" type="button" style="align-self:flex-start">＋ Add barcode</button>' +
    '</div>' +
    '</div>' +
    '<div class="row" style="margin-top:12px">' +
      '<button class="btn" id="saveBtn">Save</button>' +
      '<a class="btn ghost" href="/admin/checkin/' + u.uuid + '">🛒 Check in</a>' +
      '<button class="btn ghost" id="qrBtn">Show Handoff QR</button>' +
      '<button class="btn danger" id="delBtn">Delete</button>' +
      '<span id="estatus" class="muted small"></span>' +
    '</div>' +
    '<div id="qrBox" class="center"></div></section>';

  const bcList = document.getElementById('bcList');
  (u.barcodes || []).forEach(addBarcodeRow);
  document.getElementById('addBcBtn').onclick = () => addBarcodeRow('');
  (u.family_members || []).forEach(addFamilyMemberRow);
  document.getElementById('addFmBtn').onclick = () => addFamilyMemberRow();
  document.getElementById('saveBtn').onclick = saveUser;
  document.getElementById('delBtn').onclick = deleteUser;
  document.getElementById('cancelBtn').onclick = closeEditor;
  document.getElementById('qrBtn').onclick = toggleQR;

  // Show each label in the chosen ticket language too, e.g. "Last name ( Apellido )".
  const langSel = document.getElementById('langSel');
  langSel.addEventListener('change', () => applyFieldLabels(editorEl, langSel.value));
  applyFieldLabels(editorEl, lang);

  // Verified toggle (top-right) + zip 45424 auto-verify.
  const vctl = renderVerified(document.getElementById('verifiedSlot'), !!u.verified, v => { current.verified = v; });
  const zipInput = editorEl.querySelector('input[data-key="identity.address.zipcode"]');
  if (zipInput) zipInput.addEventListener('input', e => {
    if (e.target.value.trim() === '45424') { current.verified = true; vctl.set(true); }
  });
}

function addBarcodeRow(value) {
  const wrap = document.createElement('div');
  wrap.className = 'bc-row';
  const input = document.createElement('input');
  input.className = 'bc-input';
  input.value = value || '';
  input.placeholder = 'barcode';
  const del = document.createElement('button');
  del.type = 'button';
  del.className = 'btn ghost small';
  del.textContent = '🗑';
  del.title = 'Remove barcode';
  del.onclick = () => {
    const label = input.value.trim() || 'this empty barcode';
    if (confirm('Remove ' + label + '? (applies when you Save)')) wrap.remove();
  };
  wrap.appendChild(input);
  wrap.appendChild(del);
  document.getElementById('bcList').appendChild(wrap);
}

// Family size is calculated (read-only) = account holder + each itemized member.
// Every row counts, age or not — a member with only a sex on file is still a
// person, and is saved as one. When no members are itemized (e.g. a user created
// before this editor existed), leave the stored size untouched rather than
// collapsing it to 1.
function syncFamilySize() {
  const fs = editorEl.querySelector('input[data-key="family_size"]');
  if (!fs) return;
  const rows = document.querySelectorAll('#fmList .fm-row');
  syncFamilyCap(rows.length);
  if (rows.length === 0) return;
  fs.value = rows.length + 1;
}

// Stop the row count growing past the Max Family Size setting. An account that is
// already over it keeps every member (the server clamps growth, never what is
// stored) — the button is simply already disabled.
function syncFamilyCap(rowCount) {
  const btn = document.getElementById('addFmBtn');
  const note = document.getElementById('fmNote');
  if (!btn) return;
  if (MAXFAM <= 0) { btn.disabled = false; btn.style.opacity = ''; if (note) note.textContent = ''; return; }
  const full = rowCount >= MAXFAM - 1;
  btn.disabled = full;
  btn.style.opacity = full ? '.45' : '';
  if (note) note.textContent = full ? 'Max family size is ' + MAXFAM + ' (including this account) — set in Settings.' : '';
}

function addFamilyMemberRow(member) {
  // Guard the button path only: `member` is passed when rehydrating a stored
  // record, and those must always render in full even if they exceed the cap.
  if (!member && MAXFAM > 0 && document.querySelectorAll('#fmList .fm-row').length >= MAXFAM - 1) return;
  member = member || {};
  const wrap = document.createElement('div');
  wrap.className = 'fm-row';

  const age = document.createElement('input');
  age.type = 'number'; age.min = '0'; age.className = 'fm-age';
  if (member.age != null && member.age >= 0) age.value = member.age;

  // A visible caption, not just a placeholder: the placeholder vanishes as
  // soon as a number is typed, and then nothing says what the box is.
  const ageWrap = document.createElement('label');
  ageWrap.className = 'fm-age-wrap';
  const ageCap = document.createElement('span');
  ageCap.className = 'fm-cap';
  ageCap.textContent = 'Age';
  ageWrap.appendChild(ageCap); ageWrap.appendChild(age);

  const sex = document.createElement('select');
  sex.className = 'fm-sex';
  sex.innerHTML = '<option value="">Sex…</option><option value="male">Male</option><option value="female">Female</option>';
  if (member.sex) sex.value = member.sex;

  // The stored value stays male/female; only the visible labels swap by age, so
  // an under-18 reads Boy/Girl and 18+ reads Man/Woman — the same split the
  // check-in screen counts by. Labels stay generic until a valid age is entered.
  function syncSexLabels() {
    const a = parseInt(age.value, 10);
    const known = !isNaN(a) && a >= 0;
    sex.options[1].text = !known ? 'Male' : (a < 18 ? 'Boy' : 'Man');
    sex.options[2].text = !known ? 'Female' : (a < 18 ? 'Girl' : 'Woman');
  }

  const spouseLabel = document.createElement('label');
  spouseLabel.className = 'fm-spouse';
  const spouse = document.createElement('input');
  spouse.type = 'checkbox';
  spouse.checked = !!member.spouse;
  spouseLabel.appendChild(spouse);
  spouseLabel.appendChild(document.createTextNode('Spouse'));

  // Spouse only applies to adults (18+), matching v1.
  function syncSpouse() {
    const adult = parseInt(age.value || '0', 10) >= 18;
    spouse.disabled = !adult;
    if (!adult) spouse.checked = false;
    spouseLabel.style.opacity = adult ? '1' : '.45';
  }
  age.addEventListener('input', () => { syncSpouse(); syncSexLabels(); syncFamilySize(); });
  syncSpouse();
  syncSexLabels();

  const del = document.createElement('button');
  del.type = 'button'; del.className = 'btn ghost small'; del.textContent = '🗑'; del.title = 'Remove';
  del.onclick = () => { wrap.remove(); syncFamilySize(); };

  wrap.appendChild(ageWrap); wrap.appendChild(sex); wrap.appendChild(spouseLabel); wrap.appendChild(del);
  document.getElementById('fmList').appendChild(wrap);
  syncFamilySize();
}

// Every row on screen is a member, saved as one. Rows exist only because a
// member is on file or someone pressed ＋, so a half-filled one (age -1 and/or
// no sex — the shape v1 stored) is kept rather than silently dropped on Save:
// the check-in screen counts it as an unknown family member. Remove with 🗑.
function collectFamilyMembers() {
  return Array.from(document.querySelectorAll('#fmList .fm-row')).map(r => ({
    age: parseInt(r.querySelector('.fm-age').value || '-1', 10),
    sex: r.querySelector('.fm-sex').value,
    spouse: r.querySelector('input[type=checkbox]').checked
  }));
}

function setPath(obj, path, value) {
  const keys = path.split('.');
  let o = obj;
  for (let i = 0; i < keys.length - 1; i++) { o[keys[i]] = o[keys[i]] || {}; o = o[keys[i]]; }
  o[keys[keys.length - 1]] = value;
}

async function saveUser() {
  editorEl.querySelectorAll('input[data-key]').forEach(el => {
    const key = el.dataset.key;
    if (key === 'family_size') setPath(current, key, parseInt(el.value || '1', 10));
    else setPath(current, key, el.value);
  });
  current.barcodes = Array.from(editorEl.querySelectorAll('.bc-input')).map(i => i.value.trim()).filter(Boolean);
  current.family_members = collectFamilyMembers();
  const lang = document.getElementById('langSel').value;
  current.language = lang;
  current.spanish = (lang === 'es');

  const st = document.getElementById('estatus');
  st.textContent = 'Saving…';
  const data = await (await fetch('/api/clients/' + encodeURIComponent(current.uuid), {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(current)
  })).json();
  st.innerHTML = data.result ? '✓ Saved' : '<span class="err">' + (data.error || 'Error') + '</span>';
  if (data.user) current = data.user;
  // If we got here via the check-in page's Edit button, go back to it with the
  // same user pulled up once the save succeeds.
  if (data.result && cameFromCheckin) {
    window.location = '/admin/checkin/' + current.uuid;
  }
}

function toggleQR() {
  const box = document.getElementById('qrBox');
  const btn = document.getElementById('qrBtn');
  if (box.innerHTML) { box.innerHTML = ''; box.classList.remove('has-qr'); btn.textContent = 'Show Handoff QR'; }
  else { box.classList.add('has-qr'); renderHandoffQR(box, (current.barcodes || [])[0], { size: 240 }); btn.textContent = 'Hide Handoff QR'; }
}

function closeEditor() {
  // Came from the check-in page? Return to it with this user selected, mirroring
  // Save — so Cancel doesn't strand you back on an empty Users page.
  if (current && cameFromCheckin) { window.location = '/admin/checkin/' + current.uuid; return; }
  editorEl.innerHTML = '';
  current = null;
  history.replaceState(null, '', '/admin/users'); // drop the deep-link uuid
  // Closing a deep-linked editor leaves a plain Users page, not a blank one.
  editorOnly = false;
  showList(true);
  loadRecent();
}

async function deleteUser() {
  if (!confirm('Delete ' + (current.name_string || 'this user') + ' permanently?')) return;
  const data = await (await fetch('/api/admin/clients/' + encodeURIComponent(current.uuid) + '/delete', { method: 'POST' })).json();
  if (data.ok) { closeEditor(); search(); }
}

document.getElementById('searchBtn').onclick = search;
document.getElementById('q').addEventListener('input', liveSearch);
document.getElementById('q').addEventListener('keydown', e => { if (e.key === 'Enter') search(); });

// Load supported languages, then honor a /<uuid> deep-link (Edit from check-in).
Promise.all([
  fetch('/api/print/languages').then(r => r.json()).then(d => { if (d.result && d.result.length) LANGS = d.result; }),
  fetch('/api/limits').then(r => r.json()).then(d => { MAXFAM = parseInt((d.result || {}).max_family_size || 0, 10) || 0; })
].map(p => p.catch(() => {})))
  .finally(() => {
    const presel = (location.pathname.match(/\/admin\/users\/([^\/?#]+)/) || [])[1] || '';
    if (presel) {
      editorOnly = true;
      showList(false);
      loadUser(presel);
      return;
    }
    loadRecent();
  });
