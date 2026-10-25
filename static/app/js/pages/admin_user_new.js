// Script for admin_user_new.html, moved out of the page so the Content-Security-Policy
// can refuse inline scripts. Order relative to the page's other scripts is
// unchanged.
let FORM = null;

userFormOptions().then(opts => {
  // The verified badge sits in the page header here, so hand the form that slot
  // instead of letting it render its own.
  FORM = renderUserForm(document.getElementById('userForm'),
    Object.assign({ verifiedSlot: document.getElementById('verifiedSlot') }, opts));
  FORM.focus();
});

document.getElementById('createBtn').onclick = async function () {
  const msg = document.getElementById('msg');
  if (!FORM) return;
  if (!FORM.valid()) { msg.innerHTML = '<span class="err">First and last name are required.</span>'; return; }
  msg.textContent = 'Creating…';
  const res = await fetch('/api/clients', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(FORM.collect())
  });
  const data = await res.json();
  if (!res.ok || !data.result) { msg.innerHTML = '<span class="err">' + (data.error || 'Error') + '</span>'; return; }
  const u = data.result;
  document.getElementById('formWrap').style.display = 'none';
  document.getElementById('done').style.display = 'block';
  document.getElementById('who').textContent = u.name_string;
  document.getElementById('checkinLink').href = '/admin/checkin/' + u.uuid;
  // Straight back into the record just typed — /admin/users/<uuid> deep-links the
  // Users page into that user's editor.
  document.getElementById('editLink').href = '/admin/users/' + u.uuid;
  renderHandoffQR(document.getElementById('qr'), (u.barcodes || [])[0], {
    onURL: function (fresh) { const a = document.getElementById('freshLink'); a.href = fresh; a.textContent = fresh; }
  });
};
