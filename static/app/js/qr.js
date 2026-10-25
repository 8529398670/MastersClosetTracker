// The branded QR code -- the cross (verified.png) in the center and the maroon
// classy-rounded dots. The ticket prints the same design in black and white
// (server/printer/qr.go). Shared by the staff pages and the public site.

// drawBrandedQR draws data as the branded QR code into container, as a PNG
// <img> rather than inline SVG, so the browser's own right-click menu works on
// it: open in a new tab, copy, save. It is drawn at twice the size shown, to
// stay sharp on high-density screens. Returns the QRCodeStyling instance.
function drawBrandedQR(container, data, opts) {
  opts = opts || {};
  var size = opts.size || 300;
  if (typeof QRCodeStyling === 'undefined') {
    container.textContent = 'QR library failed to load';
    return null;
  }
  var qr = new QRCodeStyling({
    width: size * 2,
    height: size * 2,
    type: 'canvas',
    data: data,
    image: '/app/img/verified.png',
    qrOptions: { errorCorrectionLevel: 'Q' },
    dotsOptions: { color: '#913C67', type: 'classy-rounded' },
    cornersSquareOptions: { color: '#913C67', type: 'extra-rounded' },
    backgroundOptions: { color: opts.background || '#e9ebee' },
    imageOptions: { crossOrigin: 'anonymous', margin: 8 } // 4px at the size shown
  });
  container.innerHTML = '';
  var image = new Image(size, size);
  image.alt = opts.alt || 'QR code';
  image.className = 'rounded';
  container.appendChild(image);
  qr.getRawData('png').then(function (blob) {
    // One live image per container: let go of the last one's memory.
    if (container._qrURL) URL.revokeObjectURL(container._qrURL);
    container._qrURL = URL.createObjectURL(blob);
    image.src = container._qrURL;
  });
  return qr;
}

// renderHandoffQR (staff pages) draws a family's phone pass link: the public
// site's /p page with their barcode after the "#", which never leaves the
// phone. Scanning it with a phone camera saves the pass there, offline.
// opts.onURL(url) is told the link once it is known.
var _sitePublicURL = null;
function renderHandoffQR(container, barcode, opts) {
  opts = opts || {};
  if (!barcode) { container.textContent = 'This family has no barcode yet.'; return; }
  container.textContent = 'Loading…';
  var site = _sitePublicURL !== null ? Promise.resolve(_sitePublicURL)
    : fetch('/api/site').then(function (r) { return r.json(); })
      .then(function (d) { _sitePublicURL = d.public_url || ''; return _sitePublicURL; });
  site.then(function (base) {
    if (!base) { container.textContent = 'No public site is set (guests.public_url in config.yaml).'; return; }
    var url = base + '/p#' + encodeURIComponent(barcode);
    if (opts.onURL) opts.onURL(url);
    drawBrandedQR(container, url, opts);
  }).catch(function () { container.textContent = 'Could not make a QR code'; });
}
