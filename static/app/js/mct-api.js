// mct-api.js -- loaded first on every page that talks to the backend.
//
// It wraps window.fetch so the pages' own code keeps calling fetch() as it
// always has, while each request picks up what the server now requires:
//
//   - a state-changing request to /api/ carries the session's CSRF token in
//     an X-CSRF-Token header (fetched once from /api/me);
//   - a 401 from /api/ means the session has ended, so the browser goes to
//     /account to sign in again instead of the page hanging on "Loading…".
//
// The public sign-up and the guest status calls need neither.
(function () {
  var original = window.fetch.bind(window);
  var tokenPromise = null;

  function csrfToken(refresh) {
    if (refresh || !tokenPromise) {
      tokenPromise = original('/api/me', { credentials: 'same-origin' })
        .then(function (r) { return r.ok ? r.json() : {}; })
        .then(function (d) { return d.csrf_token || ''; })
        .catch(function () { return ''; });
    }
    return tokenPromise;
  }

  function isStaffAPI(url) {
    return url.origin === window.location.origin &&
      url.pathname.indexOf('/api/') === 0 &&
      url.pathname.indexOf('/api/public/') !== 0 &&
      url.pathname.indexOf('/api/guest/') !== 0;
  }

  async function send(input, init, refresh) {
    var options = Object.assign({}, init || {});
    var method = (options.method || 'GET').toUpperCase();
    var url = new URL(typeof input === 'string' ? input : input.url, window.location.href);
    if (url.origin === window.location.origin && !options.credentials) options.credentials = 'same-origin';
    if (isStaffAPI(url) && method !== 'GET' && method !== 'HEAD') {
      var headers = new Headers(options.headers || {});
      headers.set('X-CSRF-Token', await csrfToken(refresh));
      if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
      options.headers = headers;
    }
    return original(input, options);
  }

  window.fetch = async function (input, init) {
    var response = await send(input, init, false);
    var url = new URL(typeof input === 'string' ? input : input.url, window.location.href);
    if (!isStaffAPI(url)) return response;
    if (response.status === 403 && (init || {}).method && (init.method || '').toUpperCase() !== 'GET') {
      // A token from an earlier session (signed in again in another tab):
      // fetch the current one and try once more.
      var body = await response.clone().json().catch(function () { return {}; });
      if (/csrf/i.test(body.error || '')) response = await send(input, init, true);
    }
    if (response.status === 401) window.location.href = '/account';
    return response;
  };
})();
