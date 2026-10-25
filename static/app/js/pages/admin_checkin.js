// Script for admin_checkin.html, moved out of the page so the Content-Security-Policy
// can refuse inline scripts. Order relative to the page's other scripts is
// unchanged.
const lookup = document.getElementById('lookup');
const results = document.getElementById('results');
let uuid = null;
let CUR = null;      // currently loaded user (for the verified toggle)
let BALCFG = null;   // balance config from settings, for ticket defaults
let LANGS = [];      // [{code,name,rtl,strings}] — same source the printer uses
let TLANG = 'en';    // currently selected ticket language
// Selected-user deep-link comes from the path: /admin/checkin/<uuid>
const pathUuid = (location.pathname.match(/\/admin\/checkin\/([^\/?#]+)/) || [])[1] || '';

// Scripts that preview correctly but don't yet print correctly (gofpdf has no
// bidi / contextual shaping). The on-screen ticket is right; the label isn't.
const PRINT_WARN = { ar: 1, fa: 1, ur: 1, hi: 1 };

function num(id) { return parseInt(document.getElementById(id).value || '0', 10); }
function esc(v) { return v == null ? '' : String(v).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;'); }

// People on file. The cached family_size can lag behind the itemized members
// (legacy records, a size never recomputed), so trust whichever is larger.
function familyCount(u) {
  if (!u) return 1;
  return Math.max(parseInt(u.family_size, 10) || 1, 1 + (u.family_members || []).length);
}

function langData(code) { return LANGS.find(l => l.code === code) || LANGS.find(l => l.code === 'en') || null; }
function plural(n, sing, plur) { return n === 1 ? sing : plur; }

// Split a "...%d..." format into the text before and after the number.
function splitFmt(fmt) { const i = (fmt || '').indexOf('%d'); return i < 0 ? [fmt || '', ''] : [fmt.slice(0, i), fmt.slice(i + 2)]; }

// Set a line's surrounding text without touching its input (preserves focus).
function setLine(id, fmt, rtl) {
  const el = document.getElementById(id);
  if (!el) return;
  const parts = splitFmt(fmt);
  el.querySelector('.pre').textContent = parts[0];
  el.querySelector('.post').textContent = parts[1];
  el.dir = rtl ? 'rtl' : 'ltr';
}

// Re-render the ticket's localized text for the selected language.
function renderTicketText() {
  const d = langData(TLANG); if (!d) return;
  const s = d.strings, rtl = !!d.rtl;
  setLine('line_fam', s.family_size, rtl);
  setLine('line_total', s.total_items, rtl);
  const gN = countVal('guests');
  // Untranslated languages fall back to English per form, like the printer does.
  const en = (langData('en') || {}).strings || {};
  setLine('line_guests', plural(gN, s.guests_line_singular || en.guests_line_singular, s.guests_line_plural || en.guests_line_plural) || 'Guests ( %d )', rtl);
  const hp = document.getElementById('line_perperson'); hp.textContent = s.per_person; hp.dir = rtl ? 'rtl' : 'ltr';
  setLine('line_clothing', s.clothing_items, rtl);
  setLine('line_pants', s.pants_limit, rtl);
  setLine('line_shoes', '( %d ) ' + plural(num('t_shoes'), s.shoe_singular, s.shoe_plural), rtl);
  setLine('line_acc', '( %d ) ' + plural(num('t_acc'), s.accessory_singular, s.accessory_plural), rtl);
  setLine('line_seasonal', '( %d ) ' + plural(num('t_seasonal'), s.seasonal_singular, s.seasonal_plural), rtl);
  const note = document.getElementById('langPrintNote');
  if (PRINT_WARN[TLANG]) { note.style.display = ''; note.textContent = '⚠ Preview is correct, but this script does not print correctly yet.'; }
  else { note.style.display = 'none'; }
  fitPreview(); // shrink any overflowing line so it stays on one line (like print)
}

// Shrink one full-width line's font until it fits on a single line. Resets to
// the CSS default first so it can grow back when the text gets shorter.
function fitLineEl(el, minPx) {
  if (!el) return;
  el.style.whiteSpace = 'nowrap';
  el.style.fontSize = '';
  let px = parseFloat(getComputedStyle(el).fontSize), g = 0;
  while (el.scrollWidth > el.clientWidth && px > minPx && g++ < 60) { px -= 0.5; el.style.fontSize = px + 'px'; }
}

// Keep the ticket lines on one line, mirroring the printer's auto-fit. The
// per-person block shrinks as a group so it stays visually uniform.
function fitPreview() {
  fitLineEl(document.getElementById('line_fam'), 11);
  fitLineEl(document.getElementById('line_total'), 8);
  fitLineEl(document.getElementById('line_guests'), 8);
  const tper = document.querySelector('.t-per'), ticket = document.querySelector('.ticket');
  if (!tper || !ticket) return;
  const cs = getComputedStyle(ticket);
  const innerW = ticket.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
  tper.style.whiteSpace = 'nowrap';
  tper.style.fontSize = '';
  let px = parseFloat(getComputedStyle(tper).fontSize), g = 0;
  while (tper.offsetWidth > innerW && px > 8 && g++ < 60) { px -= 0.5; tper.style.fontSize = px + 'px'; }
}

// Load the ticket strings once (the same source the printer uses). The language
// itself is locked to the user's saved language — it's only editable on the
// user's Edit page — so there is no picker here.
async function loadTicketStrings() {
  try { LANGS = (await (await fetch('/api/print/strings')).json()).result || []; } catch (e) { LANGS = []; }
}

// Per-person limits come straight from settings; the total scales with people.
function setTicketFromConfig() {
  if (!BALCFG) return;
  const g = BALCFG.general || {};
  document.getElementById('t_clothing').value = g.total || 6;
  document.getElementById('t_pants').value = g.bottoms || 0;
  document.getElementById('t_shoes').value = BALCFG.shoes || 0;
  document.getElementById('t_acc').value = BALCFG.accessories || 0;
  document.getElementById('t_seasonal').value = BALCFG.seasonals || 0;
  recomputeTotal();
}
function recomputeTotal() {
  document.getElementById('t_total').textContent = (num('t_clothing') || 0) * num('t_for'); // 0 people -> 0 total
}
function debounce(fn, ms) { let t; return function () { clearTimeout(t); const a = arguments; t = setTimeout(() => fn.apply(null, a), ms); }; }

// --- "Shopping for" overview -------------------------------------------------
// Builds toggle/count chips from the user's family members. All edits here are
// temporary (per-visit): they drive the live ticket's people count, never the
// stored record. Adults are 18+, matching v1. The account holder (Self) has no
// known sex, so it's a person but not part of the men/women breakdown.
let SHOP = null;
const ADULT = 18;

function deriveShop(u) {
  let boys = 0, girls = 0, men = 0, women = 0, unknown = 0, spouse = null;
  (u.family_members || []).forEach(m => {
    const age = parseInt(m.age, 10);
    // Every stored member is a person. The ones that can't be placed in the
    // age/sex breakdown — v1 created members as { age: -1, sex: "" }, and a row
    // can still be saved without a sex — go to Unknown instead of vanishing.
    if (isNaN(age) || age < 0) { unknown++; return; }
    if (!spouse && m.spouse && age >= ADULT) { spouse = m; return; } // pull spouse out
    if (age < ADULT) { if (m.sex === 'male') boys++; else if (m.sex === 'female') girls++; else unknown++; }
    else { if (m.sex === 'male') men++; else if (m.sex === 'female') women++; else unknown++; }
  });
  return { boys, girls, men, women, unknown, spouse };
}

function spouseLabel(s) {
  if (s && s.sex === 'male') return 'Husband';
  if (s && s.sex === 'female') return 'Wife';
  return 'Spouse';
}

function sfToggle(label, on, cb) {
  const b = document.createElement('button');
  b.type = 'button';
  const paint = () => { b.className = 'btn small sf-toggle' + (b.dataset.on === '1' ? '' : ' ghost'); b.textContent = (b.dataset.on === '1' ? '✓ ' : '') + label; };
  b.dataset.on = on ? '1' : '0';
  paint();
  b.onclick = () => { b.dataset.on = b.dataset.on === '1' ? '0' : '1'; paint(); cb(b.dataset.on === '1'); };
  return b;
}

// singular/plural label that follows the count live (1 → "Boy", else "Boys").
// Pass plural = null for a fixed label that never pluralizes (e.g. Unknown).
function sfCount(singular, plural, kind, initial, extraClass) {
  const g = document.createElement('span');
  g.className = 'sf-group' + (extraClass ? ' ' + extraClass : '');
  const lab = document.createElement('label');
  const inp = document.createElement('input');
  inp.type = 'number'; inp.min = '0'; inp.value = initial;
  inp.dataset.kind = kind;
  const syncLabel = () => { lab.textContent = (plural && parseInt(inp.value || '0', 10) !== 1) ? plural : singular; };
  inp.addEventListener('input', () => { syncLabel(); recomputeShopFor(); });
  syncLabel();
  g.appendChild(lab); g.appendChild(inp);
  return g;
}

function countVal(kind) {
  const el = document.querySelector('#shopChips input[data-kind="' + kind + '"]');
  return el ? (parseInt(el.value || '0', 10) || 0) : 0;
}

function renderShoppingFor(u) {
  const d = deriveShop(u);
  SHOP = { self: true, spouseOn: !!d.spouse, spousePresent: !!d.spouse, spouseSex: d.spouse ? d.spouse.sex : '' };
  const box = document.getElementById('shopChips');
  box.innerHTML = '';

  box.appendChild(sfToggle('Self', true, on => { SHOP.self = on; recomputeShopFor(); }));
  if (d.spouse) box.appendChild(sfToggle(spouseLabel(d.spouse), true, on => { SHOP.spouseOn = on; recomputeShopFor(); }));
  if (d.boys)  box.appendChild(sfCount('Boy', 'Boys', 'boys', d.boys));
  if (d.girls) box.appendChild(sfCount('Girl', 'Girls', 'girls', d.girls));
  if (d.men)   box.appendChild(sfCount('Man', 'Men', 'men', d.men));
  if (d.women) box.appendChild(sfCount('Woman', 'Women', 'women', d.women));
  // Everything that isn't itemized by age/sex lands in one Unknown chip: the
  // stored members we couldn't classify, plus any shortfall against the cached
  // family size (users created before the family-members editor existed). The
  // ticket therefore starts at the real family size and stays adjustable.
  const accounted = 1 + (d.spouse ? 1 : 0) + d.boys + d.girls + d.men + d.women + d.unknown;
  const others = d.unknown + Math.max(0, (parseInt(u.family_size, 10) || 1) - accounted);
  if (others) {
    const c = sfCount('Unknown', null, 'unknown', others);
    c.querySelector('input').title = 'Family members without an age/sex on file';
    box.appendChild(c);
  }
  box.appendChild(sfCount('🎟 Guest', '🎟 Guests', 'guests', 0, 'sf-guests')); // separate: own ticket, no allowance change

  recomputeShopFor();
}

// People on the family ticket = Self + Spouse + non-spouse boys/girls/men/women.
// Guests are excluded (they print their own ticket). Drives #t_for + the total.
function recomputeShopFor() {
  if (!SHOP) return;
  let people = (SHOP.self ? 1 : 0) + (SHOP.spousePresent && SHOP.spouseOn ? 1 : 0)
    + countVal('boys') + countVal('girls') + countVal('men') + countVal('women') + countVal('unknown');
  if (people < 0) people = 0; // 0 is allowed — no family ticket prints, but guests still can
  document.getElementById('t_for').value = people;
  recomputeTotal();
  // Guests never change the family's allowance. They ride along as a count on the
  // family ticket, and only get labels of their own when the opt-in is ticked.
  const g = countVal('guests');
  document.getElementById('t_guests').textContent = g;
  // Guests printing their own tickets are not also counted on the family's.
  const ownTickets = document.getElementById('guestPrint').checked;
  document.getElementById('line_guests').style.display = g > 0 && !ownTickets ? '' : 'none';
  renderTicketText(); // g flips Guest/Guests, and re-fits the ticket
  const wrap = document.getElementById('guestPrintWrap');
  if (wrap) wrap.style.display = g > 0 ? 'flex' : 'none';
  const gn = document.getElementById('guestNote');
  if (gn) {
    const own = document.getElementById('guestPrint').checked;
    const parts = [];
    if (people === 0) parts.push('⚠ No family ticket will print (shopping for 0).');
    if (g > 0) {
      if (own) parts.push('🎟 + ' + g + ' separate guest ticket' + (g === 1 ? '' : 's') + ' will also print (same barcode).');
      else if (people === 0) parts.push('🎟 ' + g + ' guest' + (g === 1 ? '' : 's') + ' recorded, but nothing will print — tick the box for guest labels.');
      else parts.push('🎟 ' + g + ' guest' + (g === 1 ? '' : 's') + ' shown on the family ticket — no separate labels.');
    }
    gn.textContent = parts.join('   ');
  }
}

// Demographic breakdown for the record: the spouse (if shopping) folds into
// men/women by sex; Self stays uncounted (unknown sex). Guests are separate.
function shopDemographics() {
  let men = countVal('men'), women = countVal('women');
  if (SHOP && SHOP.spousePresent && SHOP.spouseOn) {
    if (SHOP.spouseSex === 'male') men++;
    else if (SHOP.spouseSex === 'female') women++;
  }
  return { boys: countVal('boys'), girls: countVal('girls'), men, women, guests: countVal('guests') };
}

function showFlash(html, bad) {
  const f = document.getElementById('flash');
  f.className = 'status ' + (bad ? 'wait' : 'ok');
  f.innerHTML = html;
  f.style.display = '';
}
function hideFlash() {
  const f = document.getElementById('flash');
  f.style.display = 'none';
  f.innerHTML = '';
}

// Per-guest labels are an explicit, per-visit choice: always start unticked so a
// one-off "print guest tickets too" never carries into the next family.
function resetGuestPrint() {
  const cb = document.getElementById('guestPrint');
  if (cb) cb.checked = false;
  const wrap = document.getElementById('guestPrintWrap');
  if (wrap) wrap.style.display = 'none';
}

// Hand the page back empty and focused, ready for the next scan. This now runs
// automatically after every print, so it has to clear the DOM-only state too —
// load() overwrites most fields, but never #result, #guestNote or #shopChips.
function clearAll() {
  lookup.value = '';
  results.innerHTML = '';
  document.getElementById('panel').style.display = 'none';
  resetQR();
  hideFlash();
  resetGuestPrint();
  newUserClose();
  document.getElementById('result').innerHTML = '';
  document.getElementById('who').textContent = '';
  document.getElementById('cooloff').textContent = '';
  document.getElementById('cooloff').className = 'status';
  document.getElementById('guestNote').textContent = '';
  document.getElementById('shopChips').innerHTML = '';
  document.getElementById('verifiedSlot').innerHTML = '';
  document.querySelector('#balance tbody').innerHTML = '';
  document.getElementById('line_guests').style.display = 'none';
  uuid = null; CUR = null; BALCFG = null; SHOP = null; TLANG = 'en';
  history.replaceState(null, '', '/admin/checkin'); // drop the deep-link uuid
  lookup.focus();
}

// --- inline New User --------------------------------------------------------
// The form is built lazily on first open (one fetch for languages + the family
// cap), then reused. Creating loads the new family straight into the check-in
// panel, so a walk-in goes from "not in the system" to "ready to Print" without
// ever leaving this page.
let NEWFORM = null;
const newUserPanel = document.getElementById('newUserPanel');

function newUserOpen() {
  newUserPanel.style.display = 'block';
  document.getElementById('panel').style.display = 'none';
  results.innerHTML = '';
  document.getElementById('newUserMsg').textContent = '';
  const build = NEWFORM
    ? Promise.resolve()
    : userFormOptions().then(opts => {
        // This page already has the language list from /api/print/strings
        // (code + name), so don't make the form fetch it a second time.
        if (LANGS.length) opts = Object.assign({}, opts, { languages: LANGS });
        // Badge goes top-right beside the "New User" title, as on the Edit page.
        opts = Object.assign({}, opts, { verifiedSlot: document.getElementById('newUserVerified') });
        NEWFORM = renderUserForm(document.getElementById('newUserForm'), opts);
      });
  return build.then(() => { NEWFORM.focus(); newUserPanel.scrollIntoView({ behavior: 'smooth' }); });
}

// A family's pre-signup code (made on the public site's /join page, see
// presignup.js), typed in by the scanner: open New User filled with their
// answers for the volunteer to check and Create.
//
// quiet is the paste / pause path: a code that does not decode yet may just
// be half-typed by the scanner, so leave it alone and let Enter decide.
let presignupOpening = '';
async function openPresignup(text, quiet) {
  if (presignupOpening === text) return; // Enter and the pause both fired
  presignupOpening = text;
  let record;
  try { record = await decodePresignup(text); }
  catch (e) {
    presignupOpening = '';
    if (!quiet) showFlash('⚠ That sign-up code could not be read — try scanning it again.', true);
    return;
  }
  lookup.value = '';
  results.innerHTML = '';
  NEWFORM = null; // start from a blank form, not whatever was typed before
  document.getElementById('newUserForm').innerHTML = '';
  await newUserOpen();
  NEWFORM.fill(record);
  showFlash('✍ Filled in from ' + esc(record.identity.first_name + ' ' + record.identity.last_name) + "'s sign-up code — check it with them, then Create.");
  presignupOpening = '';
}

// The digits from a ticket's phone pass link (<site>/p#<barcode>), when the
// ticket's own QR code is scanned at the desk instead of the barcode.
function passLinkBarcode(text) {
  const match = /\/p#([A-Za-z0-9-]{1,32})\s*$/.exec(text || '');
  return match ? match[1] : '';
}

function newUserClose() {
  newUserPanel.style.display = 'none';
  document.getElementById('newUserMsg').textContent = '';
}

async function createUser() {
  if (!NEWFORM) return;
  const msg = document.getElementById('newUserMsg');
  if (!NEWFORM.valid()) { msg.innerHTML = '<span class="err">First and last name are required.</span>'; return; }
  msg.textContent = 'Creating…';
  const res = await fetch('/api/clients', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(NEWFORM.collect())
  });
  const data = await res.json();
  if (!res.ok || !data.result) { msg.innerHTML = '<span class="err">' + (data.error || 'Error') + '</span>'; return; }
  const u = data.result;
  NEWFORM = null;                                   // next open starts blank
  document.getElementById('newUserForm').innerHTML = '';
  newUserClose();
  await load(u.uuid);                               // pull them up, ready to Print
  showFlash('✓ Created ' + esc(u.name_string) + ' — ready to check in');
}

document.getElementById('newUserBtn').onclick = newUserOpen;
document.getElementById('cancelUserBtn').onclick = () => {
  newUserClose();
  // Cancelling shouldn't strand whoever was pulled up before the form opened.
  // (clearAll also closes the panel, but there the reset is the point.)
  if (uuid) document.getElementById('panel').style.display = 'block';
};
document.getElementById('createUserBtn').onclick = createUser;

// Edit / Handoff QR actions for the selected user (same as the Users page).
document.getElementById('editBtn').onclick = () => { if (uuid) window.location = '/admin/users/' + uuid; };
function resetQR() {
  const box = document.getElementById('qrBox');
  box.innerHTML = '';
  box.classList.remove('has-qr');
  document.getElementById('qrBtn').textContent = '📱 Show Handoff QR';
}
document.getElementById('qrBtn').onclick = () => {
  if (!uuid) return;
  const box = document.getElementById('qrBox');
  if (box.innerHTML) { resetQR(); }
  else { box.classList.add('has-qr'); renderHandoffQR(box, CUR && (CUR.barcodes || [])[0], { size: 240 }); document.getElementById('qrBtn').textContent = '🙈 Hide Handoff QR'; }
};

// --- lookup paths -----------------------------------------------------------
async function lookupByBarcode(v) {
  results.innerHTML = '';
  const res = await fetch('/api/clients/by-barcode?code=' + encodeURIComponent(v));
  if (res.ok) { const u = await res.json(); return load(u.uuid); }
  return searchByName(v, true); // unknown barcode -> fall back to a name search
}

async function searchByName(v, autoSelect) {
  if (!v || v.length < 2) { results.innerHTML = ''; return; }
  const res = await fetch('/api/clients/search?q=' + encodeURIComponent(v));
  const users = (await res.json()).result || [];
  if (autoSelect && users.length === 1) { results.innerHTML = ''; return load(users[0].uuid); }
  renderResults(users);
}

function renderResults(users) {
  results.innerHTML = '';
  if (!users.length) { results.innerHTML = '<p class="muted small">No match.</p>'; return; }
  users.slice(0, 12).forEach(u => {
    const d = document.createElement('div');
    d.className = 'list-row';
    d.innerHTML = '<strong>' + (u.name_string || u.username || '(no name)') + '</strong>' +
      '<span class="muted small">' + ((u.barcodes && u.barcodes[0]) || '') + '</span>';
    d.onclick = () => { results.innerHTML = ''; load(u.uuid); };
    results.appendChild(d);
  });
}

// --- input handling: scanner vs human typing --------------------------------
// Live: a name (starts with letters) searches as you type. A long numeric run
// is treated as a barcode. Either way, Enter forces the decision immediately,
// and a fast keystroke burst (a scanner) is always treated as a barcode.
const liveSearch = debounce(function () {
  const v = lookup.value.trim();
  if (v.length < 2) { results.innerHTML = ''; return; }
  // A pasted (or fully scanned) sign-up code opens on its own; one still
  // arriving is left for Enter. Same for a ticket's pass link.
  if (isPresignup(v)) return openPresignup(v, true);
  const passBarcode = passLinkBarcode(v);
  if (passBarcode) { lookup.value = passBarcode; return lookupByBarcode(passBarcode); }
  if (/^MC1/i.test(v) || /^https?:/i.test(v)) return;
  if (/^\d{6,}$/.test(v)) return lookupByBarcode(v); // numeric barcode (no Enter scanners)
  if (/^\d+$/.test(v)) return;                       // short digit run — wait for more
  searchByName(v, false);
}, 220);
lookup.addEventListener('input', liveSearch);

let firstKeyAt = 0, keyCount = 0;
lookup.addEventListener('keydown', function (e) {
  if (e.key === 'Enter') {
    e.preventDefault();
    const v = lookup.value.trim();
    const scanned = keyCount >= 4 && (Date.now() - firstKeyAt) < 150;
    firstKeyAt = 0; keyCount = 0;
    if (!v) return;
    if (isPresignup(v)) return openPresignup(v);
    const passBarcode = passLinkBarcode(v);
    if (passBarcode) { lookup.value = passBarcode; return lookupByBarcode(passBarcode); }
    if (scanned || /^\d+$/.test(v)) return lookupByBarcode(v);
    return searchByName(v, true);
  }
  if (e.key.length === 1) { if (keyCount === 0) firstKeyAt = Date.now(); keyCount++; }
});
document.getElementById('clearBtn').onclick = clearAll;

// --- selected user ----------------------------------------------------------
async function load(id) {
  uuid = id;
  history.replaceState(null, '', '/admin/checkin/' + id); // keep URL navigable/back-able
  resetQR();
  hideFlash();          // the last person's confirmation is done with
  resetGuestPrint();    // decided fresh for this family
  newUserClose();
  document.getElementById('result').innerHTML = '';
  const data = await (await fetch('/api/clients/' + encodeURIComponent(id) + '/checkin')).json();
  const u = data.user, out = data.result;
  CUR = u;
  BALCFG = data.balance_config || null;
  // The phone pass QR, beside the name as the printer places it. On screen it
  // is the branded handoff QR; the label gets the black-and-white version.
  document.getElementById('t_bottom').classList.toggle('with-qr', !!data.handoff_qr);
  const ticketQR = document.getElementById('t_qr');
  ticketQR.innerHTML = '';
  if (data.handoff_qr) renderHandoffQR(ticketQR, (u.barcodes || [])[0], { size: 96 });
  document.getElementById('panel').style.display = 'block';
  const fam = familyCount(u);
  document.getElementById('who').textContent = u.name_string + '  ·  family of ' + fam;
  document.getElementById('t_for').value = fam; // default people = family size
  document.getElementById('t_name').value = u.name_string || '';
  document.getElementById('t_barcode').textContent = (u.barcodes && u.barcodes[0]) ? u.barcodes[0] : 'barcode assigned at check-in';
  setTicketFromConfig();                                       // per-person limits from settings
  renderShoppingFor(u);                                        // who's shopping -> drives t_for + total
  // Lock the ticket language to the user's saved language, then localize.
  TLANG = u.language || (u.spanish ? 'es' : 'en');
  if (!langData(TLANG)) TLANG = 'en';
  renderTicketText();
  // Read-only badge: verified state is only editable from the Edit page,
  // like every other user attribute. (No onToggle -> renderVerified draws a
  // plain badge instead of a button.)
  renderVerified(document.getElementById('verifiedSlot'), !!u.verified);
  const co = document.getElementById('cooloff');
  if (out.allowed) { co.className = 'status ok'; co.textContent = '✓ Cleared to check in'; }
  else { co.className = 'status wait'; co.textContent = '⏳ Last visit too recent — ' + out.days_remaining + ' day(s) of cooloff left'; }
  renderBalance(u.balance);
  document.getElementById('panel').scrollIntoView({ behavior: 'smooth' });
}

function renderBalance(b) {
  const rows = [
    ['Tops', b.general.tops], ['Bottoms', b.general.bottoms], ['Dresses', b.general.dresses],
    ['Shoes', b.shoes], ['Seasonals', b.seasonals], ['Accessories', b.accessories]
  ];
  document.querySelector('#balance tbody').innerHTML = rows.map(function (r) {
    const it = r[1];
    return '<tr><td>' + r[0] + '</td><td>' + it.available + '</td><td>' + it.limit + '</td><td>' + it.used + '</td></tr>';
  }).join('');
}

// again = the volunteer confirmed checking this family in a second time
// moments after the first.
async function checkin(again) {
  const req = Object.assign({
    shopping_for: num('t_for'),
    clothing_per_person: num('t_clothing'),
    pants_limit: num('t_pants'),
    shoes_limit: num('t_shoes'),
    accessories_limit: num('t_acc'),
    seasonal_limit: num('t_seasonal'),
    family_name: document.getElementById('t_name').value.trim(),
    language: TLANG, // print in the language shown in the preview
    print_guest_tickets: document.getElementById('guestPrint').checked,
    again: again === true
  }, shopDemographics()); // boys/girls/men/women/guests from the overview
  const r = document.getElementById('result');
  r.textContent = 'Printing…';
  const res = await fetch('/api/clients/' + encodeURIComponent(uuid) + '/visits', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(req)
  });
  const data = await res.json();
  if (data.recent) { showRecent(data.recent_visit_id); return; }
  if (!res.ok || !data.result) { r.innerHTML = '<span class="err">' + (data.error || 'Error') + '</span>'; return; }
  await afterPrint(data);
}

// The family was checked in moments ago. Offer to reprint that visit's ticket
// (no new visit), or to record a second visit anyway.
function showRecent(visitId) {
  const r = document.getElementById('result');
  r.innerHTML = '';
  const row = document.createElement('div');
  row.className = 'row';
  if (visitId) {
    const reprint = document.createElement('button');
    reprint.type = 'button'; reprint.className = 'btn accent small';
    reprint.textContent = '🖨 Reprint that ticket';
    reprint.onclick = () => reprintVisit(visitId);
    row.appendChild(reprint);
  }
  const again = document.createElement('button');
  again.type = 'button'; again.className = 'btn accent ghost small';
  again.textContent = 'Check in again anyway';
  again.onclick = () => checkin(true);
  row.appendChild(again);
  r.appendChild(row);
}

async function reprintVisit(visitId) {
  const r = document.getElementById('result');
  r.textContent = 'Printing…';
  const res = await fetch('/api/visits/' + encodeURIComponent(visitId) + '/print', { method: 'POST' });
  const data = await res.json();
  if (!res.ok || !data.result) { r.innerHTML = '<span class="err">' + esc(data.error || 'Error') + '</span>'; return; }
  await afterPrint(data);
}

// Report a print (a new check-in or a reprint) and reset for the next family.
async function afterPrint(data) {
  const r = document.getElementById('result');
  const ulid = data.check_in.ULID;
  let printMsg;
  if (data.check_in.shopping.shopping_for === 0) printMsg = '<span class="muted">No family ticket (shopping for 0)</span>';
  else if (data.printed) printMsg = '🖨 Ticket printed';
  else if (data.print_error) printMsg = '<span class="err">⚠ Print failed: ' + data.print_error + '</span>';
  else printMsg = '<span class="muted">(no printer configured)</span>';
  if (data.guests_printed) printMsg += ' · 🎟 ' + data.guests_printed + ' guest ticket' + (data.guests_printed === 1 ? '' : 's');
  const msg = '✓ ' + esc(data.check_in.name || 'Checked in') + ' — ' + data.check_in.date + ' ' + data.check_in.time +
    ' · ' + printMsg + ' · <a href="/api/visits/' + encodeURIComponent(ulid) + '/ticket.pdf" target="_blank">View / reprint ticket</a>';
  // The visit is recorded either way, but a failed print keeps the family on
  // screen so the printer can be sorted out. Refresh the balances FIRST — load()
  // clears #result — then leave the error sitting there. Pressing Print again
  // would record a second visit, so point at the ticket link instead.
  if (data.print_error) {
    await load(uuid);
    r.innerHTML = msg + ' <span class="muted">— the visit is already recorded; re-print from the ticket link rather than pressing Print again.</span>';
    return;
  }
  // Printed (or there was nothing to print): reset for the next person and carry
  // the confirmation up to the flash line, which outlives the panel.
  clearAll();
  showFlash(msg);
}
document.getElementById('checkinBtn').onclick = () => checkin(false);
document.getElementById('guestPrint').addEventListener('change', recomputeShopFor); // note follows the choice
document.getElementById('t_for').addEventListener('input', recomputeTotal);      // people changes -> total updates
document.getElementById('t_clothing').addEventListener('input', recomputeTotal); // clothing/person changes -> total updates
// Shoe/accessory/seasonal counts flip singular/plural wording — re-localize.
['t_shoes', 't_acc', 't_seasonal'].forEach(id =>
  document.getElementById(id).addEventListener('input', renderTicketText));

// Load the ticket strings once, then honor a /<uuid> deep-link.
loadTicketStrings().finally(() => { if (pathUuid) load(pathUuid); });
