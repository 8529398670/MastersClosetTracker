// The pre-signup code: a family's sign-up form, packed into text short enough
// for a QR code on their phone. The public site's /join page encodes it
// (static/site/join.js); the check-in desk decodes what the scanner types and
// fills the New User form (static/app/js/pages/admin_checkin.js). Nothing is
// ever sent anywhere -- the code is the only copy until staff press Create.
//
// Format: "MC1Z-" + base32(deflate-raw(json)), or "MC1P-" + base32(json) on a
// phone too old to compress. The JSON is a positional array (see pack), so
// field names cost nothing. Base32 (A-Z, 2-7) because a keyboard-wedge
// scanner types it faithfully even with Caps Lock on or an odd keyboard
// layout, accented names are safe inside the bytes, and QR codes store those
// characters in their compact alphanumeric mode.
//
// Changing the layout means a new version: bump the "1" in the prefix and
// keep decoding the old one, since codes saved on phones live for months.

var PRESIGNUP_PREFIX = /^MC1([ZP])-([A-Z2-7]+)$/;
var BASE32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

function base32Encode(bytes) {
  var out = '', bits = 0, value = 0;
  for (var i = 0; i < bytes.length; i++) {
    value = (value << 8) | bytes[i]; bits += 8;
    while (bits >= 5) { out += BASE32[(value >>> (bits - 5)) & 31]; bits -= 5; }
  }
  if (bits > 0) out += BASE32[(value << (5 - bits)) & 31];
  return out;
}

function base32Decode(text) {
  var out = [], bits = 0, value = 0;
  for (var i = 0; i < text.length; i++) {
    var index = BASE32.indexOf(text[i]);
    if (index < 0) throw new Error('not a pre-signup code');
    value = (value << 5) | index; bits += 5;
    if (bits >= 8) { out.push((value >>> (bits - 8)) & 255); bits -= 8; }
  }
  return new Uint8Array(out);
}

async function streamBytes(bytes, transform) {
  var stream = new Blob([bytes]).stream().pipeThrough(transform);
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

// pack/unpack map the record (the shape user_form.js's collect() and fill()
// use) to and from the positional array.
function pack(record) {
  var id = record.identity || {}, a = id.address || {};
  return [
    id.first_name || '', id.middle_name || '', id.last_name || '',
    record.phone_number || '', record.email_address || '',
    [a.street_number || '', a.street_name || '', a.city || '', a.state || '', a.zipcode || ''],
    record.language || 'en',
    (record.family_members || []).map(function (m) {
      return [m.age == null ? -1 : m.age, m.sex === 'male' ? 'm' : m.sex === 'female' ? 'f' : '', m.spouse ? 1 : 0];
    })
  ];
}

function unpack(list) {
  if (!Array.isArray(list)) throw new Error('not a pre-signup code');
  var a = list[5] || [];
  return {
    identity: {
      first_name: list[0] || '', middle_name: list[1] || '', last_name: list[2] || '',
      address: { street_number: a[0] || '', street_name: a[1] || '', city: a[2] || '', state: a[3] || '', zipcode: a[4] || '' }
    },
    phone_number: list[3] || '', email_address: list[4] || '',
    language: list[6] || 'en',
    family_members: (list[7] || []).map(function (m) {
      return { age: typeof m[0] === 'number' ? m[0] : -1, sex: m[1] === 'm' ? 'male' : m[1] === 'f' ? 'female' : '', spouse: m[2] === 1 };
    })
  };
}

async function encodePresignup(record) {
  var bytes = new TextEncoder().encode(JSON.stringify(pack(record)));
  if (typeof CompressionStream !== 'undefined') {
    try { return 'MC1Z-' + base32Encode(await streamBytes(bytes, new CompressionStream('deflate-raw'))); }
    catch (e) { /* fall through to the uncompressed form */ }
  }
  return 'MC1P-' + base32Encode(bytes);
}

// isPresignup is a cheap test for what the scanner just typed.
function isPresignup(text) {
  return PRESIGNUP_PREFIX.test(String(text || '').replace(/\s+/g, '').toUpperCase());
}

async function decodePresignup(text) {
  var match = PRESIGNUP_PREFIX.exec(String(text || '').replace(/\s+/g, '').toUpperCase());
  if (!match) throw new Error('not a pre-signup code');
  var bytes = base32Decode(match[2]);
  if (match[1] === 'Z') bytes = await streamBytes(bytes, new DecompressionStream('deflate-raw'));
  return unpack(JSON.parse(new TextDecoder().decode(bytes)));
}
