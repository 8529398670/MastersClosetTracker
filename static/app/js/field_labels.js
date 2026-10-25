// Translated form-field labels for the user editor and the New User form.
//
// When a guest's ticket language isn't English, each field label also shows the
// word in that language — "Last name ( Apellido )" — so the volunteer can point at
// a field and the guest can read what it's asking for. Keys match the codes in
// printer/lang.go (langOrder); a language or key missing here just shows English.
//
// Like the ticket strings, these are machine/LLM-generated and should be spot-
// checked by a native speaker.
//
// Usage: put <span class="field-tr" data-lk="last_name"></span> in a label, then
// call applyFieldLabels(root, langCode) after rendering and whenever the language
// picker changes.

const FIELD_LABELS = {
  es: {
    first_name: 'Nombre', middle_name: 'Segundo nombre', last_name: 'Apellido',
    email: 'Correo electrónico', phone: 'Teléfono', family_size: 'Tamaño de la familia',
    street_number: 'Número de calle', street_name: 'Nombre de la calle', city: 'Ciudad',
    state: 'Estado', zip: 'Código postal', language: 'Idioma',
    family_members: 'Miembros de la familia', barcodes: 'Códigos de barras'
  },
  fr: {
    first_name: 'Prénom', middle_name: 'Deuxième prénom', last_name: 'Nom de famille',
    email: 'E-mail', phone: 'Téléphone', family_size: 'Taille de la famille',
    street_number: 'Numéro de rue', street_name: 'Nom de rue', city: 'Ville',
    state: 'État', zip: 'Code postal', language: 'Langue',
    family_members: 'Membres de la famille', barcodes: 'Codes-barres'
  },
  de: {
    first_name: 'Vorname', middle_name: 'Zweiter Vorname', last_name: 'Nachname',
    email: 'E-Mail', phone: 'Telefon', family_size: 'Familiengröße',
    street_number: 'Hausnummer', street_name: 'Straße', city: 'Stadt',
    state: 'Bundesstaat', zip: 'Postleitzahl', language: 'Sprache',
    family_members: 'Familienmitglieder', barcodes: 'Barcodes'
  },
  pt: {
    first_name: 'Nome', middle_name: 'Nome do meio', last_name: 'Sobrenome',
    email: 'E-mail', phone: 'Telefone', family_size: 'Tamanho da família',
    street_number: 'Número', street_name: 'Nome da rua', city: 'Cidade',
    state: 'Estado', zip: 'CEP', language: 'Idioma',
    family_members: 'Membros da família', barcodes: 'Códigos de barras'
  },
  it: {
    first_name: 'Nome', middle_name: 'Secondo nome', last_name: 'Cognome',
    email: 'Email', phone: 'Telefono', family_size: 'Dimensione della famiglia',
    street_number: 'Numero civico', street_name: 'Via', city: 'Città',
    state: 'Stato', zip: 'CAP', language: 'Lingua',
    family_members: 'Membri della famiglia', barcodes: 'Codici a barre'
  },
  nl: {
    first_name: 'Voornaam', middle_name: 'Tweede naam', last_name: 'Achternaam',
    email: 'E-mail', phone: 'Telefoon', family_size: 'Gezinsgrootte',
    street_number: 'Huisnummer', street_name: 'Straatnaam', city: 'Stad',
    state: 'Staat', zip: 'Postcode', language: 'Taal',
    family_members: 'Gezinsleden', barcodes: 'Barcodes'
  },
  pl: {
    first_name: 'Imię', middle_name: 'Drugie imię', last_name: 'Nazwisko',
    email: 'E-mail', phone: 'Telefon', family_size: 'Wielkość rodziny',
    street_number: 'Numer domu', street_name: 'Ulica', city: 'Miasto',
    state: 'Stan', zip: 'Kod pocztowy', language: 'Język',
    family_members: 'Członkowie rodziny', barcodes: 'Kody kreskowe'
  },
  ro: {
    first_name: 'Prenume', middle_name: 'Al doilea prenume', last_name: 'Nume de familie',
    email: 'E-mail', phone: 'Telefon', family_size: 'Mărimea familiei',
    street_number: 'Număr', street_name: 'Strada', city: 'Oraș',
    state: 'Stat', zip: 'Cod poștal', language: 'Limbă',
    family_members: 'Membrii familiei', barcodes: 'Coduri de bare'
  },
  tr: {
    first_name: 'Ad', middle_name: 'İkinci ad', last_name: 'Soyadı',
    email: 'E-posta', phone: 'Telefon', family_size: 'Aile büyüklüğü',
    street_number: 'Kapı numarası', street_name: 'Sokak adı', city: 'Şehir',
    state: 'Eyalet', zip: 'Posta kodu', language: 'Dil',
    family_members: 'Aile üyeleri', barcodes: 'Barkodlar'
  },
  sw: {
    first_name: 'Jina la kwanza', middle_name: 'Jina la kati', last_name: 'Jina la ukoo',
    email: 'Barua pepe', phone: 'Simu', family_size: 'Ukubwa wa familia',
    street_number: 'Namba ya nyumba', street_name: 'Jina la mtaa', city: 'Jiji',
    state: 'Jimbo', zip: 'Msimbo wa posta', language: 'Lugha',
    family_members: 'Wanafamilia', barcodes: 'Misimbo pau'
  },
  ht: {
    first_name: 'Prenon', middle_name: 'Dezyèm prenon', last_name: 'Non fanmi',
    email: 'Imèl', phone: 'Telefòn', family_size: 'Gwosè fanmi',
    street_number: 'Nimewo kay', street_name: 'Non lari', city: 'Vil',
    state: 'Eta', zip: 'Kòd postal', language: 'Lang',
    family_members: 'Manm fanmi', barcodes: 'Kòd ba'
  },
  ru: {
    first_name: 'Имя', middle_name: 'Второе имя', last_name: 'Фамилия',
    email: 'Эл. почта', phone: 'Телефон', family_size: 'Размер семьи',
    street_number: 'Номер дома', street_name: 'Улица', city: 'Город',
    state: 'Штат', zip: 'Почтовый индекс', language: 'Язык',
    family_members: 'Члены семьи', barcodes: 'Штрихкоды'
  },
  uk: {
    first_name: "Ім'я", middle_name: "Друге ім'я", last_name: 'Прізвище',
    email: 'Ел. пошта', phone: 'Телефон', family_size: "Розмір сім'ї",
    street_number: 'Номер будинку', street_name: 'Вулиця', city: 'Місто',
    state: 'Штат', zip: 'Поштовий індекс', language: 'Мова',
    family_members: "Члени сім'ї", barcodes: 'Штрихкоди'
  },
  el: {
    first_name: 'Όνομα', middle_name: 'Μεσαίο όνομα', last_name: 'Επώνυμο',
    email: 'Email', phone: 'Τηλέφωνο', family_size: 'Μέγεθος οικογένειας',
    street_number: 'Αριθμός', street_name: 'Οδός', city: 'Πόλη',
    state: 'Πολιτεία', zip: 'Ταχυδρομικός κώδικας', language: 'Γλώσσα',
    family_members: 'Μέλη οικογένειας', barcodes: 'Γραμμωτοί κώδικες'
  },
  vi: {
    first_name: 'Tên', middle_name: 'Tên đệm', last_name: 'Họ',
    email: 'Email', phone: 'Điện thoại', family_size: 'Số người trong gia đình',
    street_number: 'Số nhà', street_name: 'Tên đường', city: 'Thành phố',
    state: 'Tiểu bang', zip: 'Mã bưu điện', language: 'Ngôn ngữ',
    family_members: 'Thành viên gia đình', barcodes: 'Mã vạch'
  },
  ar: {
    first_name: 'الاسم الأول', middle_name: 'الاسم الأوسط', last_name: 'اسم العائلة',
    email: 'البريد الإلكتروني', phone: 'الهاتف', family_size: 'حجم العائلة',
    street_number: 'رقم المنزل', street_name: 'اسم الشارع', city: 'المدينة',
    state: 'الولاية', zip: 'الرمز البريدي', language: 'اللغة',
    family_members: 'أفراد العائلة', barcodes: 'الباركود'
  },
  fa: {
    first_name: 'نام', middle_name: 'نام میانی', last_name: 'نام خانوادگی',
    email: 'ایمیل', phone: 'تلفن', family_size: 'اندازه خانواده',
    street_number: 'پلاک', street_name: 'نام خیابان', city: 'شهر',
    state: 'ایالت', zip: 'کد پستی', language: 'زبان',
    family_members: 'اعضای خانواده', barcodes: 'بارکدها'
  },
  ur: {
    first_name: 'پہلا نام', middle_name: 'درمیانی نام', last_name: 'خاندانی نام',
    email: 'ای میل', phone: 'فون', family_size: 'خاندان کا حجم',
    street_number: 'مکان نمبر', street_name: 'گلی کا نام', city: 'شہر',
    state: 'ریاست', zip: 'زپ کوڈ', language: 'زبان',
    family_members: 'خاندان کے افراد', barcodes: 'بارکوڈز'
  },
  hi: {
    first_name: 'पहला नाम', middle_name: 'मध्य नाम', last_name: 'उपनाम',
    email: 'ईमेल', phone: 'फ़ोन', family_size: 'परिवार का आकार',
    street_number: 'मकान नंबर', street_name: 'सड़क का नाम', city: 'शहर',
    state: 'राज्य', zip: 'पिन कोड', language: 'भाषा',
    family_members: 'परिवार के सदस्य', barcodes: 'बारकोड'
  }
};

// fieldTr returns the empty placeholder span a label carries for its translation.
function fieldTr(lk) {
  return '<span class="field-tr" data-lk="' + lk + '"></span>';
}

// applyFieldLabels fills (or, for English/unknown codes, clears) every .field-tr
// placeholder under root. <bdi> keeps RTL scripts from flipping the parentheses.
function applyFieldLabels(root, lang) {
  const t = FIELD_LABELS[lang] || {};
  root.querySelectorAll('.field-tr[data-lk]').forEach(el => {
    const word = t[el.dataset.lk];
    el.textContent = '';
    if (!word) return;
    const bdi = document.createElement('bdi');
    bdi.textContent = word;
    el.append('( ', bdi, ' )');
  });
}
