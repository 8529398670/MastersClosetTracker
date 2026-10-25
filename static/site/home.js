// The landing page: a phone that has saved its pass sees it straight away,
// above the calendar; one with a sign-up code waiting sees that instead.
//
// The ticket's QR code links here as /#<barcode>. The number after "#" never
// reaches the server; it is read here, kept on this phone, and dropped from
// the address bar.
(function () {
  var fromLink = '';
  try { fromLink = decodeURIComponent(location.hash.replace(/^#/, '')).trim(); } catch (e) {}
  if (validBarcode(fromLink)) {
    siteSet(SITE_KEYS.pass, fromLink);
    siteSet(SITE_KEYS.presignup, ''); // they have a real record now
  }
  if (location.hash) history.replaceState(null, '', '/');
  var badLink = fromLink !== '' && !validBarcode(fromLink);

  var saved = document.getElementById('saved');
  var card = document.getElementById('savedCard');
  var joinLink = document.getElementById('joinLink');
  function show() {
    // Kept on the phone when storage works; this visit's link when it does not.
    var barcode = siteGet(SITE_KEYS.pass) || (validBarcode(fromLink) ? fromLink : '');
    var code = siteGet(SITE_KEYS.presignup);
    if (validBarcode(barcode)) {
      card.innerHTML = '<h2>My Pass</h2><div id="passBody"></div>';
      renderPassCard(document.getElementById('passBody'), barcode, { onForget: function () { fromLink = ''; show(); } });
      saved.hidden = false; joinLink.hidden = true;
    } else if (code) {
      card.innerHTML = '<h2>My sign-up code</h2><div id="codeBody"></div>';
      renderCodeCard(document.getElementById('codeBody'), code, { onForget: show });
      saved.hidden = false; joinLink.hidden = true;
    } else if (badLink) {
      card.innerHTML = '';
      card.appendChild(el('p', 'muted', "That link couldn't be read. Scan the QR code on your ticket again, or ask at the front desk."));
      saved.hidden = false; joinLink.hidden = false;
    } else {
      card.innerHTML = '';
      saved.hidden = true; joinLink.hidden = false;
    }
  }
  show();
  registerOffline();
  // A second pass link opened while this page is already up changes only the
  // "#", which does not reload the page: start over so it is read.
  window.addEventListener('hashchange', function () { location.reload(); });
})();
