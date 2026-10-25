// The shared "new user" form.
//
// It lives here rather than inside a page because the check-in desk needs to add a
// walk-in without navigating away, so the same form now renders in two places. The
// fiddly parts — family-member rows that recalculate family size, the max-family
// cap from settings, barcode rows, the ticket language, zip-45424 auto-verify —
// would drift immediately if they were maintained as two copies.
//
// Usage:
//   const opts = await userFormOptions();          // languages + limits, cached
//   const form = renderUserForm(container, opts);  // paints into container
//   const body = form.collect();                   // -> POST /api/clients
//
// Requires verified.js (renderVerified) and field_labels.js to be loaded first.

let _userFormOptions = null;

// Fetch the language list and the family cap once per page load. Both fall back to
// something usable so a failed request degrades to "no cap, English only" rather
// than an unusable form.
async function userFormOptions() {
  if (_userFormOptions) return _userFormOptions;
  const get = async (url, fallback) => {
    try {
      const d = await (await fetch(url)).json();
      return d.result || fallback;
    } catch (e) { return fallback; }
  };
  const [languages, limits] = await Promise.all([
    get('/api/print/languages', [{ code: 'en', name: 'English' }]),
    get('/api/limits', {})
  ]);
  _userFormOptions = {
    languages: languages,
    maxFamilySize: parseInt(limits.max_family_size || 0, 10) || 0 // 0 = no limit
  };
  return _userFormOptions;
}

function ufEsc(v) {
  return v == null ? '' : String(v).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
}

// lk is the field_labels.js key for the label's translation.
function ufField(label, name, lk, extra) {
  return '<label class="field"><span class="field-head">' + label + fieldTr(lk) + '</span>' +
    '<input name="' + name + '"' + (extra || '') + '></label>';
}

// renderUserForm paints the form into container and returns a controller.
//   opts.languages      [{code,name}] for the ticket-language picker
//   opts.maxFamilySize  largest family incl. the account holder; 0 = no limit
//   opts.verifiedSlot   element to host the verified badge (default: inside the form)
function renderUserForm(container, opts) {
  opts = opts || {};
  const langs = (opts.languages && opts.languages.length) ? opts.languages : [{ code: 'en', name: 'English' }];
  const max = parseInt(opts.maxFamilySize || 0, 10) || 0;
  const maxMembers = max > 0 ? max - 1 : 0; // the account holder occupies one slot

  const langOpts = langs.map(l =>
    '<option value="' + ufEsc(l.code) + '"' + (l.code === 'en' ? ' selected' : '') + '>' + ufEsc(l.name) + '</option>'
  ).join('');

  // The badge renders in the caller's header when it gives a slot; otherwise in
  // its own row here, top-right like everywhere else.
  container.innerHTML =
    (opts.verifiedSlot ? '' : '<div class="row" style="justify-content:flex-end;margin-bottom:8px"><span class="uf-verified"></span></div>') +
    '<div class="grid">' +
      ufField('First name', 'first_name', 'first_name', ' required') +
      ufField('Middle name', 'middle_name', 'middle_name') +
      ufField('Last name', 'last_name', 'last_name', ' required') +
      ufField('Email', 'email_address', 'email', ' type="email"') +
      ufField('Phone', 'phone_number', 'phone', ' type="tel"') +
      ufField('Family size', 'family_size', 'family_size', ' type="number" value="1" min="1"' + (max > 0 ? ' max="' + max + '"' : '')) +
      ufField('Street #', 'street_number', 'street_number') +
      ufField('Street name', 'street_name', 'street_name') +
      ufField('City', 'city', 'city') +
      ufField('State', 'state', 'state') +
      ufField('Zip', 'zipcode', 'zip') +
      '<label class="field"><span class="field-head">Ticket language' + fieldTr('language') + '</span>' +
        '<select name="language">' + langOpts + '</select></label>' +
    '</div>' +
    '<div class="field"><span class="field-head">Family members' + fieldTr('family_members') + '</span>' +
      '<div class="uf-fm"></div>' +
      '<button class="btn ghost small uf-add-fm" type="button" style="align-self:flex-start">＋ Add family member</button>' +
      '<span class="uf-fm-note muted small"></span>' +
    '</div>' +
    '<div class="field"><span class="field-head">Barcodes' + fieldTr('barcodes') + '</span>' +
      '<div class="uf-bc"></div>' +
      '<button class="btn ghost small uf-add-bc" type="button" style="align-self:flex-start">＋ Add barcode</button>' +
    '</div>';

  const q = sel => container.querySelector(sel);
  const fmList = q('.uf-fm'), bcList = q('.uf-bc');
  const addFmBtn = q('.uf-add-fm'), fmNote = q('.uf-fm-note');
  const sizeInput = container.querySelector('input[name="family_size"]');

  let verified = false;
  const vctl = renderVerified(opts.verifiedSlot || q('.uf-verified'), false, v => { verified = v; });

  // --- barcodes -------------------------------------------------------------
  function addBarcodeRow(value) {
    const wrap = document.createElement('div');
    wrap.className = 'bc-row';
    const input = document.createElement('input');
    input.className = 'bc-input'; input.value = value || ''; input.placeholder = 'barcode';
    const del = document.createElement('button');
    del.type = 'button'; del.className = 'btn ghost small'; del.textContent = '🗑'; del.title = 'Remove';
    del.onclick = () => { if (confirm('Remove ' + (input.value.trim() || 'this empty barcode') + '?')) wrap.remove(); };
    wrap.appendChild(input); wrap.appendChild(del);
    bcList.appendChild(wrap);
  }
  q('.uf-add-bc').onclick = () => addBarcodeRow('');

  // --- family members -------------------------------------------------------
  // Family size mirrors the account holder + every member row (a row with only a
  // sex on file is still a person, and is saved as one), capped by the Max Family
  // Size setting.
  function syncFamilySize() {
    const rows = fmList.querySelectorAll('.fm-row');
    let size = rows.length + 1;
    if (max > 0 && size > max) size = max;
    if (sizeInput) sizeInput.value = size;
    syncCap(rows.length);
  }

  // The cap is enforced on the server too (user.ClampFamily); this is just so the
  // button says no before the volunteer types a whole row that gets dropped.
  function syncCap(rowCount) {
    if (max <= 0) { addFmBtn.disabled = false; fmNote.textContent = ''; return; }
    const full = rowCount >= maxMembers;
    addFmBtn.disabled = full;
    addFmBtn.style.opacity = full ? '.45' : '';
    fmNote.textContent = full
      ? 'Max family size is ' + max + ' (including this account) — set in Settings.'
      : '';
  }

  function addFamilyMemberRow(member) {
    if (max > 0 && fmList.querySelectorAll('.fm-row').length >= maxMembers) return;
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
    fmList.appendChild(wrap);
    syncFamilySize();
  }
  addFmBtn.onclick = () => addFamilyMemberRow();
  syncCap(0);

  // Show each label in the chosen ticket language too, e.g. "Last name ( Apellido )".
  const langSel = container.querySelector('select[name="language"]');
  langSel.addEventListener('change', () => applyFieldLabels(container, langSel.value));
  applyFieldLabels(container, langSel.value);

  // Locals (zip 45424) auto-verify.
  container.querySelector('input[name="zipcode"]').addEventListener('input', e => {
    if (e.target.value.trim() === '45424') { verified = true; vctl.set(true); }
  });

  // Every row on screen is a member, saved as one — a half-filled row (age -1
  // and/or no sex) is kept rather than dropped, matching the family size shown
  // above and the edit page. Remove with 🗑.
  function collectFamilyMembers() {
    return Array.from(fmList.querySelectorAll('.fm-row')).map(r => ({
      age: parseInt(r.querySelector('.fm-age').value || '-1', 10),
      sex: r.querySelector('.fm-sex').value,
      spouse: r.querySelector('input[type=checkbox]').checked
    }));
  }

  function val(name) {
    const el = container.querySelector('[name="' + name + '"]');
    return el ? (el.value || '').trim() : '';
  }

  return {
    // collect returns the POST body for /api/clients. `spanish` is kept in
    // step with `language` for the legacy field, as the edit page does.
    collect() {
      const lang = val('language') || 'en';
      let size = parseInt(val('family_size') || '1', 10) || 1;
      if (max > 0 && size > max) size = max;
      return {
        identity: {
          first_name: val('first_name'), middle_name: val('middle_name'), last_name: val('last_name'),
          address: {
            street_number: val('street_number'), street_name: val('street_name'),
            city: val('city'), state: val('state'), zipcode: val('zipcode')
          }
        },
        email_address: val('email_address'),
        phone_number: val('phone_number'),
        family_size: size,
        family_members: collectFamilyMembers(),
        barcodes: Array.from(bcList.querySelectorAll('.bc-input')).map(i => i.value.trim()).filter(Boolean),
        language: lang,
        spanish: lang === 'es',
        verified: verified
      };
    },
    // fill loads a record shaped like collect()'s into the form -- a family's
    // pre-signup code (presignup.js) scanned at the desk. Staff review it and
    // press Create as usual.
    fill(record) {
      record = record || {};
      const identity = record.identity || {}, address = identity.address || {};
      const set = (name, value) => {
        const el = container.querySelector('[name="' + name + '"]');
        if (el && value != null && value !== '') el.value = value;
      };
      set('first_name', identity.first_name); set('middle_name', identity.middle_name); set('last_name', identity.last_name);
      set('email_address', record.email_address); set('phone_number', record.phone_number);
      set('street_number', address.street_number); set('street_name', address.street_name);
      set('city', address.city); set('state', address.state); set('zipcode', address.zipcode);
      if (record.language && langSel.querySelector('option[value="' + record.language + '"]')) {
        langSel.value = record.language;
        applyFieldLabels(container, langSel.value);
      }
      fmList.innerHTML = '';
      (record.family_members || []).forEach(m => addFamilyMemberRow(m));
      syncFamilySize();
      if (String(address.zipcode || '').trim() === '45424') { verified = true; vctl.set(true); }
    },
    // valid mirrors the two `required` fields without needing a real <form>.
    valid() { return val('first_name') !== '' && val('last_name') !== ''; },
    focus() { const el = container.querySelector('[name="first_name"]'); if (el) el.focus(); }
  };
}
