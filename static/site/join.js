// The offline pre-signup form. Submitting packs the answers into a code
// (static/app/js/presignup.js) and shows it as a QR code; the front desk scans
// it on the family's first visit and the New User form fills itself in.
// Nothing leaves the phone.
(function () {
  // The ticket languages, as the printer lists them (server/printer/lang.go,
  // langOrder). The site has no API to ask, so keep this in step by hand.
  var LANGUAGES = [
    ['en', 'English'], ['es', 'Spanish (Español)'], ['fr', 'French (Français)'], ['de', 'German (Deutsch)'],
    ['pt', 'Portuguese (Português)'], ['it', 'Italian (Italiano)'], ['nl', 'Dutch (Nederlands)'], ['pl', 'Polish (Polski)'],
    ['ro', 'Romanian (Română)'], ['tr', 'Turkish (Türkçe)'], ['sw', 'Swahili (Kiswahili)'], ['ht', 'Haitian Creole (Kreyòl Ayisyen)'],
    ['ru', 'Russian (Русский)'], ['uk', 'Ukrainian (Українська)'], ['el', 'Greek (Ελληνικά)'], ['vi', 'Vietnamese (Tiếng Việt)'],
    ['ar', 'Arabic (العربية)'], ['fa', 'Persian (فارسی)'], ['ur', 'Urdu (اردو)'], ['hi', 'Hindi (हिन्दी)']
  ];
  var MAX_MEMBERS = 30;

  var form = document.getElementById('form');
  var language = document.getElementById('language');
  var members = document.getElementById('members');
  var msg = document.getElementById('msg');
  LANGUAGES.forEach(function (pair) {
    var option = document.createElement('option');
    option.value = pair[0]; option.textContent = pair[1];
    language.appendChild(option);
  });
  language.addEventListener('change', function () { applyFieldLabels(form, language.value); });

  function addMember(member) {
    if (members.children.length >= MAX_MEMBERS) return;
    member = member || {};
    var row = el('div', 'fm-row');
    var age = el('input'); age.type = 'number'; age.min = '0'; age.max = '120';
    if (member.age != null && member.age >= 0) age.value = member.age;
    // A visible caption: a placeholder would vanish once a number is typed.
    var ageWrap = el('label', 'fm-age-wrap');
    ageWrap.appendChild(el('span', 'fm-cap', 'Age')); ageWrap.appendChild(age);
    var sex = el('select');
    sex.innerHTML = '<option value="">Sex…</option><option value="male">Male</option><option value="female">Female</option>';
    if (member.sex) sex.value = member.sex;
    var spouseLabel = el('label');
    var spouse = el('input'); spouse.type = 'checkbox'; spouse.checked = !!member.spouse;
    spouseLabel.appendChild(spouse); spouseLabel.appendChild(document.createTextNode('Spouse'));
    var remove = el('button', 'btn ghost small', '🗑'); remove.type = 'button'; remove.title = 'Remove';
    remove.onclick = function () { row.remove(); };
    // As on the staff form: under 18 reads Boy/Girl, 18+ Man/Woman (the value
    // stays male/female), and spouse only applies to adults.
    function sync() {
      var years = parseInt(age.value, 10);
      var known = !isNaN(years) && years >= 0;
      sex.options[1].text = !known ? 'Male' : (years < 18 ? 'Boy' : 'Man');
      sex.options[2].text = !known ? 'Female' : (years < 18 ? 'Girl' : 'Woman');
      var adult = known && years >= 18;
      spouse.disabled = !adult;
      if (!adult) spouse.checked = false;
      spouseLabel.style.opacity = adult ? '1' : '.45';
    }
    age.addEventListener('input', sync); sync();
    row.appendChild(ageWrap); row.appendChild(sex); row.appendChild(spouseLabel); row.appendChild(remove);
    members.appendChild(row);
  }
  document.getElementById('addMember').onclick = function () { addMember(); };

  function value(name) {
    var field = form.querySelector('[name="' + name + '"]');
    return field ? field.value.trim() : '';
  }

  // collect builds the same shape the staff form's collect() returns, so
  // the desk can fill() it straight in.
  function collect() {
    return {
      identity: {
        first_name: value('first_name'), middle_name: value('middle_name'), last_name: value('last_name'),
        address: {
          street_number: value('street_number'), street_name: value('street_name'),
          city: value('city'), state: value('state'), zipcode: value('zipcode')
        }
      },
      phone_number: value('phone_number'), email_address: value('email_address'),
      language: language.value,
      family_members: Array.prototype.map.call(members.querySelectorAll('.fm-row'), function (row) {
        return {
          age: parseInt(row.querySelector('input[type=number]').value || '-1', 10),
          sex: row.querySelector('select').value,
          spouse: row.querySelector('input[type=checkbox]').checked
        };
      })
    };
  }

  function showCode(code) {
    form.hidden = true;
    document.getElementById('result').hidden = false;
    renderCodeCard(document.getElementById('codeCard'), code, {
      onForget: function () { window.location = '/'; }
    });
    window.scrollTo(0, 0);
  }

  form.addEventListener('submit', async function (event) {
    event.preventDefault();
    if (!value('first_name') || !value('last_name')) {
      msg.innerHTML = '<span class="err">Please fill in your first and last name.</span>';
      return;
    }
    msg.textContent = '';
    var code = await encodePresignup(collect());
    siteSet(SITE_KEYS.presignup, code);
    showCode(code);
  });

  // Coming back to change answers: start from the saved code, if any.
  (async function () {
    var saved = siteGet(SITE_KEYS.presignup);
    if (saved) {
      try {
        var record = await decodePresignup(saved);
        var id = record.identity, address = id.address;
        [['first_name', id.first_name], ['middle_name', id.middle_name], ['last_name', id.last_name],
         ['phone_number', record.phone_number], ['email_address', record.email_address],
         ['street_number', address.street_number], ['street_name', address.street_name],
         ['city', address.city], ['state', address.state], ['zipcode', address.zipcode]].forEach(function (pair) {
          var field = form.querySelector('[name="' + pair[0] + '"]');
          if (field) field.value = pair[1] || '';
        });
        language.value = record.language || 'en';
        record.family_members.forEach(addMember);
      } catch (e) { /* an unreadable old code: start fresh */ }
    }
    applyFieldLabels(form, language.value);
  })();
  registerOffline();
})();
