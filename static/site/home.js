// The landing page: a phone that has saved its pass sees it straight away,
// above the calendar; one with a sign-up code waiting sees that instead.
(function () {
  var saved = document.getElementById('saved');
  var card = document.getElementById('savedCard');
  var joinLink = document.getElementById('joinLink');
  function show() {
    var barcode = siteGet(SITE_KEYS.pass);
    var code = siteGet(SITE_KEYS.presignup);
    if (validBarcode(barcode)) {
      card.innerHTML = '<h2>My Pass</h2><div id="passBody"></div>';
      renderPassCard(document.getElementById('passBody'), barcode, { onForget: show });
      saved.hidden = false; joinLink.hidden = true;
    } else if (code) {
      card.innerHTML = '<h2>My sign-up code</h2><div id="codeBody"></div>';
      renderCodeCard(document.getElementById('codeBody'), code, { onForget: show });
      saved.hidden = false; joinLink.hidden = true;
    } else {
      card.innerHTML = '';
      saved.hidden = true; joinLink.hidden = false;
    }
  }
  show();
  registerOffline();
})();
