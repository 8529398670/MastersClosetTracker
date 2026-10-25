// Shared by the public site's pages: what this phone has saved, the pass
// card the front desk scans, and saving a card as an image. Nothing here
// talks to a server; the phone is the only place a family's code lives.

var SITE_KEYS = {
  pass: 'mct-pass',           // the family's barcode number, from their ticket
  presignup: 'mct-presignup', // a pre-signup code not yet turned into a record
  mode: 'mct-pass-mode'       // whether this phone last showed the QR or the barcode
};

// Storage can be missing (private mode, blocked site data): every read and
// write is optional, and the page still works for this visit.
function siteGet(key) {
  try { return localStorage.getItem(key) || ''; } catch (e) { return ''; }
}
function siteSet(key, value) {
  try {
    if (value) localStorage.setItem(key, value);
    else localStorage.removeItem(key);
  } catch (e) { /* this visit only */ }
}

// A barcode as the station stores it: a minted one is ten digits, but cards
// imported from the old system can be any short code (00042). Letters,
// digits and dashes only -- what Code128 prints and a scanner types back.
function validBarcode(text) {
  return /^[A-Za-z0-9-]{1,32}$/.test(String(text || ''));
}

// The service worker keeps these pages on the phone, so a saved card opens
// with no signal at the front desk.
function registerOffline() {
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(function () {});
  }
}

function el(tag, className, text) {
  var node = document.createElement(tag);
  if (className) node.className = className;
  if (text != null) node.textContent = text;
  return node;
}

// renderPassCard draws the family's card into container: a QR code (the
// barcode number, which a 2D scanner types like a scanned card) or the
// Code128 barcode itself, the number, and Save / Forget buttons.
function renderPassCard(container, barcode, opts) {
  opts = opts || {};
  container.innerHTML = '';
  var tabs = el('div', 'pass-tabs');
  var qrTab = el('button', 'pass-tab', 'QR code'); qrTab.type = 'button';
  var barTab = el('button', 'pass-tab', 'Barcode'); barTab.type = 'button';
  tabs.appendChild(qrTab); tabs.appendChild(barTab);
  var qrBox = el('div', 'pass-code');
  var barBox = el('div', 'pass-code');
  var number = el('div', 'pass-number', barcode);
  var tip = el('p', 'muted small', 'Show this at the front desk. Turn your screen brightness up if it won\'t scan.');
  var actions = el('div', 'pass-actions');
  var save = el('button', 'btn small', '⬇ Save image'); save.type = 'button';
  var forget = el('button', 'btn ghost small', 'Remove from this phone'); forget.type = 'button';
  actions.appendChild(save); actions.appendChild(forget);
  [tabs, qrBox, barBox, number, tip, actions].forEach(function (node) { container.appendChild(node); });

  drawBrandedQR(qrBox, barcode, { size: 260, background: '#ffffff' });
  var svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  barBox.appendChild(svg);
  if (typeof JsBarcode !== 'undefined') {
    JsBarcode(svg, barcode, { format: 'CODE128', displayValue: false, height: 110, width: 2.6, margin: 12 });
  } else {
    barBox.textContent = 'Barcode unavailable — use the QR code.';
  }

  function setMode(mode) {
    if (mode !== 'barcode') mode = 'qr';
    qrBox.hidden = mode !== 'qr';
    barBox.hidden = mode !== 'barcode';
    qrTab.classList.toggle('on', mode === 'qr');
    barTab.classList.toggle('on', mode === 'barcode');
    siteSet(SITE_KEYS.mode, mode);
  }
  qrTab.onclick = function () { setMode('qr'); };
  barTab.onclick = function () { setMode('barcode'); };
  setMode(siteGet(SITE_KEYS.mode) || 'qr');

  save.onclick = function () { saveCardImage(barcode, 'masters-closet-' + barcode + '.png', true); };
  forget.onclick = function () {
    if (!confirm('Remove this code from this phone? Your ticket still has it.')) return;
    siteSet(SITE_KEYS.pass, '');
    if (opts.onForget) opts.onForget();
  };
}

// renderCodeCard shows a pre-signup code as one big QR code for the desk.
function renderCodeCard(container, code, opts) {
  opts = opts || {};
  container.innerHTML = '';
  var box = el('div', 'pass-code');
  container.appendChild(box);
  drawBrandedQR(box, code, { size: 300, background: '#ffffff' });
  container.appendChild(el('p', 'muted small', 'Show this at the front desk on your first visit. We\'ll scan it to set you up — nothing was sent anywhere.'));
  var actions = el('div', 'pass-actions');
  var save = el('button', 'btn small', '⬇ Save image'); save.type = 'button';
  var edit = el('a', 'btn ghost small', 'Change my answers'); edit.href = '/join';
  var forget = el('button', 'btn ghost small', 'Remove from this phone'); forget.type = 'button';
  actions.appendChild(save); actions.appendChild(edit); actions.appendChild(forget);
  container.appendChild(actions);
  save.onclick = function () { saveCardImage(code, 'masters-closet-signup.png', false); };
  forget.onclick = function () {
    if (!confirm('Remove your sign-up code from this phone?')) return;
    siteSet(SITE_KEYS.presignup, '');
    if (opts.onForget) opts.onForget();
  };
}

function loadImage(src) {
  return new Promise(function (resolve, reject) {
    var image = new Image();
    image.onload = function () { resolve(image); };
    image.onerror = reject;
    image.src = src;
  });
}

// saveCardImage draws a printable card -- logo, QR code, and for a pass the
// barcode and number too -- and hands it to the phone: the share sheet where
// there is one (iOS offers "Save Image"), a download otherwise.
async function saveCardImage(data, filename, withBarcode) {
  var width = 720, height = withBarcode ? 1080 : 900;
  var canvas = document.createElement('canvas');
  canvas.width = width; canvas.height = height;
  var ctx = canvas.getContext('2d');
  ctx.fillStyle = '#ffffff'; ctx.fillRect(0, 0, width, height);
  try {
    var logo = await loadImage('/app/img/logo.png');
    var logoWidth = 420, logoHeight = logo.height * logoWidth / logo.width;
    ctx.drawImage(logo, (width - logoWidth) / 2, 30, logoWidth, logoHeight);
  } catch (e) { /* the code matters, the logo does not */ }

  var holder = document.createElement('div');
  var qr = drawBrandedQR(holder, data, { size: 540, background: '#ffffff' });
  if (qr) {
    var blob = await qr.getRawData('png');
    var qrImage = await loadImage(URL.createObjectURL(blob));
    ctx.drawImage(qrImage, (width - 540) / 2, 230, 540, 540);
  }
  if (withBarcode && typeof JsBarcode !== 'undefined') {
    var barCanvas = document.createElement('canvas');
    JsBarcode(barCanvas, data, { format: 'CODE128', displayValue: false, height: 120, width: 3, margin: 0 });
    var barWidth = Math.min(600, barCanvas.width);
    ctx.drawImage(barCanvas, (width - barWidth) / 2, 800, barWidth, 120);
    ctx.fillStyle = '#1f2933';
    ctx.font = '600 40px ui-monospace, Menlo, Consolas, monospace';
    ctx.textAlign = 'center';
    ctx.fillText(data, width / 2, 990);
  }

  var png = await new Promise(function (resolve) { canvas.toBlob(resolve, 'image/png'); });
  var file = new File([png], filename, { type: 'image/png' });
  if (navigator.canShare && navigator.canShare({ files: [file] })) {
    try { await navigator.share({ files: [file], title: "Master's Closet" }); return; }
    catch (e) { if (e && e.name === 'AbortError') return; }
  }
  var link = document.createElement('a');
  link.href = URL.createObjectURL(png);
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
}
