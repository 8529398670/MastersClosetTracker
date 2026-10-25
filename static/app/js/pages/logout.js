// /admin/logout: end the staff session and go to the sign-in page. The
// account routes take the CSRF token in the JSON body (the template's
// convention), so it is fetched and sent there.
(async function () {
  try {
    const me = await (await fetch('/api/me')).json();
    if (me.csrf_token) {
      await fetch('/api/logout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ csrf_token: me.csrf_token })
      });
    }
  } catch (err) { /* signed out already, or offline: the sign-in page says which */ }
  window.location.replace('/account');
})();
