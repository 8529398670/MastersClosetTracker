// renderVerified mounts a "Verified / Unverified" toggle (with the verified
// icon) into `slot`, shown top-right of the account header. `state` is the
// current bool; `onToggle(newState)` fires on click (omit it for a read-only
// badge). Returns a controller with set(bool) so callers can update it (e.g.
// when a 45424 zipcode auto-verifies).
function renderVerified(slot, state, onToggle) {
  function paint() {
    slot.innerHTML = '';
    const el = document.createElement(onToggle ? 'button' : 'span');
    el.type = 'button';
    el.className = 'vbadge ' + (state ? 'on' : 'off');
    el.title = onToggle ? (state ? 'Verified — click to un-verify' : 'Click to verify') : '';
    // Verified: just the cross icon (no text). Unverified: "Un-Verified" text
    // only, no icon. (alt="" so a slow/broken image never leaks a text label.)
    el.innerHTML = state ? '<img src="/app/img/verified.png" alt="">' : '<span>Un-Verified</span>';
    if (onToggle) el.onclick = () => { state = !state; paint(); onToggle(state); };
    slot.appendChild(el);
  }
  paint();
  return { set: (v) => { if (state !== !!v) { state = !!v; paint(); } }, get: () => state };
}
