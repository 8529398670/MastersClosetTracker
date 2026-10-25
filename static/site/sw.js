// Keeps the public site on the phone, so a saved pass or sign-up code opens
// at the front desk with no signal. Network first -- the server never lets a
// page go stale, and neither should this -- falling back to the last copy
// when the phone is offline.
var CACHE = 'mct-site-v1';
var FILES = [
  '/', '/p', '/join',
  '/site/site.css', '/site/site.js', '/site/home.js', '/site/pass.js', '/site/join.js',
  '/app/css/app.css', '/app/img/logo.png', '/app/img/verified.png',
  '/app/js/qr.js', '/app/js/presignup.js', '/app/js/field_labels.js',
  '/vendor/qr-code-styling.min.js', '/vendor/JsBarcode.all.min.js',
  '/manifest.webmanifest'
];

self.addEventListener('install', function (event) {
  event.waitUntil(caches.open(CACHE).then(function (cache) { return cache.addAll(FILES); }));
  self.skipWaiting();
});

self.addEventListener('activate', function (event) {
  event.waitUntil(caches.keys().then(function (names) {
    return Promise.all(names.filter(function (name) { return name !== CACHE; })
      .map(function (name) { return caches.delete(name); }));
  }).then(function () { return self.clients.claim(); }));
});

self.addEventListener('fetch', function (event) {
  var request = event.request;
  var url = new URL(request.url);
  // Only this site's own GETs; the calendar frame and anything else pass by.
  if (request.method !== 'GET' || url.origin !== self.location.origin) return;
  // The barcode in "/p#..." is never part of the request, so one cached /p
  // serves every family's link.
  event.respondWith(fetch(request).then(function (response) {
    if (response.ok) {
      var copy = response.clone();
      caches.open(CACHE).then(function (cache) { cache.put(request, copy); });
    }
    return response;
  }).catch(function () {
    return caches.match(request, { ignoreSearch: true });
  }));
});
