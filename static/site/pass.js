// /p#<barcode> -- the link printed on a ticket. The number after "#" never
// reaches the server; it is read here, kept on this phone, and dropped from
// the address bar. With no number in the link, the phone's saved pass shows.
(function () {
  var fromLink = decodeURIComponent(location.hash.replace(/^#/, '')).trim();
  if (validBarcode(fromLink)) {
    siteSet(SITE_KEYS.pass, fromLink);
    siteSet(SITE_KEYS.presignup, ''); // they have a real record now
  }
  if (location.hash) history.replaceState(null, '', '/p');

  var badLink = fromLink !== '' && !validBarcode(fromLink);
  var barcode = validBarcode(fromLink) ? fromLink : siteGet(SITE_KEYS.pass);
  var card = document.getElementById('card');
  var empty = document.getElementById('empty');
  function show() {
    if (!validBarcode(barcode)) {
      card.innerHTML = '';
      if (badLink) empty.textContent = "That link couldn't be read. Scan the QR code on your ticket again, or ask at the front desk.";
      empty.hidden = false;
      return;
    }
    empty.hidden = true;
    renderPassCard(card, barcode, { onForget: function () { barcode = ''; show(); } });
  }
  show();
  registerOffline();
  // A second pass link opened while this page is already up changes only the
  // "#", which does not reload the page: start over so it is read.
  window.addEventListener('hashchange', function () { location.reload(); });
})();
