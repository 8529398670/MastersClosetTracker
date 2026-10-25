package printer

// Ticket localization. v1 hardcoded an English/Spanish if-branch; v2 keys the
// ticket strings by language code so adding a language is just one map entry
// (plus a Languages() row). The %d placeholders are filled at render time.
//
// Fonts: the ticket is a tiny FIXED set of ~11 phrases, so we don't machine-
// translate at runtime — each language is a pre-translated table entry. What a
// language *can print* depends on the embedded font covering its script:
//
//   Font == ""                -> ComicNeue (Latin: en/es/fr/de/pt/it/nl/pl/…)
//   Font == "NotoSans"        -> Latin-ext + Cyrillic + Greek (ru/uk/el/vi)
//   Font == "NotoSansArabic"  -> Arabic script (ar/fa/ur)   [see RTL note]
//   Font == "NotoSansDevanagari" -> Devanagari (hi)
//
// The live on-screen preview always renders correctly (the browser has system
// fonts for every script). Printing is the constrained side.
//
// RTL / complex-script caveat: gofpdf has no bidi or contextual shaping, so the
// Arabic-script and Devanagari entries render their glyphs but do NOT join /
// reorder the way those scripts require — the on-screen preview is correct, but
// the PRINTED label for ar/fa/ur/hi is not yet production-quality. Getting those
// to print correctly needs a shaping-capable PDF path (e.g. HarfBuzz), which is
// a separate piece of work. Latin, Cyrillic, Greek and Vietnamese print fine.
//
// The non-English strings below are machine/LLM-generated and should be spot-
// checked by a native speaker before going to print.

// TicketStrings holds the localized ticket phrases for one language. Fields are
// exported (with json tags) so the live preview can fetch the exact same source
// of truth the printer uses. Font/RTL are render hints, not printed text.
type TicketStrings struct {
	Font string `json:"-"`   // embedded font key; "" => the default Latin font
	RTL  bool   `json:"rtl"` // right-to-left script (preview direction hint)

	FamilySize        string `json:"family_size"`    // "...( %d )"
	TotalItems        string `json:"total_items"`    // "...( %d )"
	PerPerson         string `json:"per_person"`     //
	ClothingItems     string `json:"clothing_items"` // "( %d ) ..."
	PantsLimit        string `json:"pants_limit"`    // "...( %d )..."
	ShoeSingular      string `json:"shoe_singular"`
	ShoePlural        string `json:"shoe_plural"`
	AccessorySingular string `json:"accessory_singular"`
	AccessoryPlural   string `json:"accessory_plural"`
	SeasonalSingular  string `json:"seasonal_singular"`
	SeasonalPlural    string `json:"seasonal_plural"`
	Guest             string `json:"guest"` // "...( %d )"; printed only on a guest's own ticket
	// The guest COUNT line on a family ticket — distinct from Guest above, which
	// titles a single guest's own ticket. Pluralized like the shoe/accessory lines,
	// so one guest reads "Guest ( 1 )". Empty entries fall back to English.
	GuestsLineSingular string `json:"guests_line_singular"` // "...( %d )"
	GuestsLinePlural   string `json:"guests_line_plural"`   // "...( %d )"
}

var translations = map[string]TicketStrings{
	"en": {
		FamilySize: "Family Size ( %d )", TotalItems: "Total Clothing Items for Family ( %d )",
		PerPerson: "Per Person :", ClothingItems: "( %d ) Clothing Items", PantsLimit: "Limit ( %d ) Pants",
		ShoeSingular: "Pair of Shoes", ShoePlural: "Pairs of Shoes",
		AccessorySingular: "Accessory", AccessoryPlural: "Accessories",
		SeasonalSingular: "Seasonal Item", SeasonalPlural: "Seasonal Items",
		Guest: "Guest ( %d )", GuestsLineSingular: "Guest ( %d )", GuestsLinePlural: "Guests ( %d )",
	},
	"es": {
		FamilySize: "Tamaño Familiar ( %d )", TotalItems: "Total Vestir Para La Familia ( %d )",
		PerPerson: "Por Persona :", ClothingItems: "( %d ) Artículos de Ropa", PantsLimit: "Límite ( %d ) Pantalones",
		ShoeSingular: "Par de Zapatos", ShoePlural: "Pares de Zapatos",
		AccessorySingular: "Accesorio", AccessoryPlural: "Accesorios",
		SeasonalSingular: "Artículo de Temporada", SeasonalPlural: "Artículos de Temporada",
		Guest: "Invitado ( %d )", GuestsLineSingular: "Invitado ( %d )", GuestsLinePlural: "Invitados ( %d )",
	},
	"fr": {
		FamilySize: "Taille de la Famille ( %d )", TotalItems: "Total de Vêtements pour la Famille ( %d )",
		PerPerson: "Par Personne :", ClothingItems: "( %d ) Vêtements", PantsLimit: "Limite ( %d ) Pantalons",
		ShoeSingular: "Paire de Chaussures", ShoePlural: "Paires de Chaussures",
		AccessorySingular: "Accessoire", AccessoryPlural: "Accessoires",
		SeasonalSingular: "Article de Saison", SeasonalPlural: "Articles de Saison",
		Guest: "Invité ( %d )", GuestsLineSingular: "Invité ( %d )", GuestsLinePlural: "Invités ( %d )",
	},
	"de": {
		FamilySize: "Familiengröße ( %d )", TotalItems: "Kleidungsstücke für die Familie insgesamt ( %d )",
		PerPerson: "Pro Person :", ClothingItems: "( %d ) Kleidungsstücke", PantsLimit: "Limit ( %d ) Hosen",
		ShoeSingular: "Paar Schuhe", ShoePlural: "Paar Schuhe",
		AccessorySingular: "Accessoire", AccessoryPlural: "Accessoires",
		SeasonalSingular: "Saisonartikel", SeasonalPlural: "Saisonartikel",
		Guest: "Gast ( %d )", GuestsLineSingular: "Gast ( %d )", GuestsLinePlural: "Gäste ( %d )",
	},
	"pt": {
		FamilySize: "Tamanho da Família ( %d )", TotalItems: "Total de Roupas para a Família ( %d )",
		PerPerson: "Por Pessoa :", ClothingItems: "( %d ) Peças de Roupa", PantsLimit: "Limite ( %d ) Calças",
		ShoeSingular: "Par de Sapatos", ShoePlural: "Pares de Sapatos",
		AccessorySingular: "Acessório", AccessoryPlural: "Acessórios",
		SeasonalSingular: "Item de Estação", SeasonalPlural: "Itens de Estação",
		Guest: "Convidado ( %d )", GuestsLineSingular: "Convidado ( %d )", GuestsLinePlural: "Convidados ( %d )",
	},
	"it": {
		FamilySize: "Dimensione della Famiglia ( %d )", TotalItems: "Totale Capi per la Famiglia ( %d )",
		PerPerson: "Per Persona :", ClothingItems: "( %d ) Capi di Abbigliamento", PantsLimit: "Limite ( %d ) Pantaloni",
		ShoeSingular: "Paio di Scarpe", ShoePlural: "Paia di Scarpe",
		AccessorySingular: "Accessorio", AccessoryPlural: "Accessori",
		SeasonalSingular: "Articolo Stagionale", SeasonalPlural: "Articoli Stagionali",
		Guest: "Ospite ( %d )", GuestsLineSingular: "Ospite ( %d )", GuestsLinePlural: "Ospiti ( %d )",
	},
	"nl": {
		FamilySize: "Gezinsgrootte ( %d )", TotalItems: "Totaal Kledingstukken voor het Gezin ( %d )",
		PerPerson: "Per Persoon :", ClothingItems: "( %d ) Kledingstukken", PantsLimit: "Limiet ( %d ) Broeken",
		ShoeSingular: "Paar Schoenen", ShoePlural: "Paren Schoenen",
		AccessorySingular: "Accessoire", AccessoryPlural: "Accessoires",
		SeasonalSingular: "Seizoensartikel", SeasonalPlural: "Seizoensartikelen",
		Guest: "Gast ( %d )", GuestsLineSingular: "Gast ( %d )", GuestsLinePlural: "Gasten ( %d )",
	},
	"pl": {
		FamilySize: "Wielkość Rodziny ( %d )", TotalItems: "Łączna Liczba Ubrań dla Rodziny ( %d )",
		PerPerson: "Na Osobę :", ClothingItems: "( %d ) Sztuk Odzieży", PantsLimit: "Limit ( %d ) Spodni",
		ShoeSingular: "Para Butów", ShoePlural: "Par Butów",
		AccessorySingular: "Akcesorium", AccessoryPlural: "Akcesoria",
		SeasonalSingular: "Artykuł Sezonowy", SeasonalPlural: "Artykuły Sezonowe",
		Guest: "Gość ( %d )", GuestsLineSingular: "Gość ( %d )", GuestsLinePlural: "Goście ( %d )",
	},
	"ro": {
		FamilySize: "Mărimea Familiei ( %d )", TotalItems: "Total Articole de Îmbrăcăminte pentru Familie ( %d )",
		PerPerson: "Pe Persoană :", ClothingItems: "( %d ) Articole de Îmbrăcăminte", PantsLimit: "Limită ( %d ) Pantaloni",
		ShoeSingular: "Pereche de Pantofi", ShoePlural: "Perechi de Pantofi",
		AccessorySingular: "Accesoriu", AccessoryPlural: "Accesorii",
		SeasonalSingular: "Articol de Sezon", SeasonalPlural: "Articole de Sezon",
		Guest: "Invitat ( %d )", GuestsLineSingular: "Invitat ( %d )", GuestsLinePlural: "Invitați ( %d )",
	},
	"tr": {
		FamilySize: "Aile Büyüklüğü ( %d )", TotalItems: "Aile için Toplam Giysi ( %d )",
		PerPerson: "Kişi Başına :", ClothingItems: "( %d ) Giysi", PantsLimit: "Limit ( %d ) Pantolon",
		ShoeSingular: "Çift Ayakkabı", ShoePlural: "Çift Ayakkabı",
		AccessorySingular: "Aksesuar", AccessoryPlural: "Aksesuar",
		SeasonalSingular: "Mevsimlik Ürün", SeasonalPlural: "Mevsimlik Ürün",
		Guest: "Misafir ( %d )", GuestsLineSingular: "Misafir ( %d )", GuestsLinePlural: "Misafir ( %d )",
	},
	"sw": {
		FamilySize: "Ukubwa wa Familia ( %d )", TotalItems: "Jumla ya Nguo kwa Familia ( %d )",
		PerPerson: "Kwa Kila Mtu :", ClothingItems: "( %d ) Nguo", PantsLimit: "Kikomo ( %d ) Suruali",
		ShoeSingular: "Jozi ya Viatu", ShoePlural: "Jozi za Viatu",
		AccessorySingular: "Kifaa", AccessoryPlural: "Vifaa",
		SeasonalSingular: "Bidhaa ya Msimu", SeasonalPlural: "Bidhaa za Msimu",
		Guest: "Mgeni ( %d )", GuestsLineSingular: "Mgeni ( %d )", GuestsLinePlural: "Wageni ( %d )",
	},
	"ht": {
		FamilySize: "Gwosè Fanmi ( %d )", TotalItems: "Total Rad pou Fanmi an ( %d )",
		PerPerson: "Pou Chak Moun :", ClothingItems: "( %d ) Rad", PantsLimit: "Limit ( %d ) Pantalon",
		ShoeSingular: "Pè Soulye", ShoePlural: "Pè Soulye",
		AccessorySingular: "Akseswa", AccessoryPlural: "Akseswa",
		SeasonalSingular: "Atik Sezon", SeasonalPlural: "Atik Sezon",
		Guest: "Envite ( %d )", GuestsLineSingular: "Envite ( %d )", GuestsLinePlural: "Envite ( %d )",
	},

	// --- NotoSans: Latin-ext + Cyrillic + Greek -----------------------------
	"ru": {
		Font: "NotoSans",
		FamilySize: "Размер семьи ( %d )", TotalItems: "Всего предметов одежды для семьи ( %d )",
		PerPerson: "На человека :", ClothingItems: "( %d ) предметов одежды", PantsLimit: "Лимит ( %d ) брюк",
		ShoeSingular: "Пара обуви", ShoePlural: "Пар обуви",
		AccessorySingular: "Аксессуар", AccessoryPlural: "Аксессуары",
		SeasonalSingular: "Сезонный товар", SeasonalPlural: "Сезонные товары",
		Guest: "Гость ( %d )", GuestsLineSingular: "Гость ( %d )", GuestsLinePlural: "Гости ( %d )",
	},
	"uk": {
		Font: "NotoSans",
		FamilySize: "Розмір сім'ї ( %d )", TotalItems: "Усього одягу для сім'ї ( %d )",
		PerPerson: "На особу :", ClothingItems: "( %d ) одиниць одягу", PantsLimit: "Ліміт ( %d ) штанів",
		ShoeSingular: "Пара взуття", ShoePlural: "Пар взуття",
		AccessorySingular: "Аксесуар", AccessoryPlural: "Аксесуари",
		SeasonalSingular: "Сезонний товар", SeasonalPlural: "Сезонні товари",
		Guest: "Гість ( %d )", GuestsLineSingular: "Гість ( %d )", GuestsLinePlural: "Гості ( %d )",
	},
	"el": {
		Font: "NotoSans",
		FamilySize: "Μέγεθος Οικογένειας ( %d )", TotalItems: "Σύνολο Ρούχων για την Οικογένεια ( %d )",
		PerPerson: "Ανά Άτομο :", ClothingItems: "( %d ) Ρούχα", PantsLimit: "Όριο ( %d ) Παντελόνια",
		ShoeSingular: "Ζευγάρι Παπούτσια", ShoePlural: "Ζευγάρια Παπούτσια",
		AccessorySingular: "Αξεσουάρ", AccessoryPlural: "Αξεσουάρ",
		SeasonalSingular: "Εποχιακό Είδος", SeasonalPlural: "Εποχιακά Είδη",
		Guest: "Επισκέπτης ( %d )", GuestsLineSingular: "Επισκέπτης ( %d )", GuestsLinePlural: "Επισκέπτες ( %d )",
	},
	"vi": {
		Font: "NotoSans",
		FamilySize: "Số Người trong Gia Đình ( %d )", TotalItems: "Tổng Số Quần Áo cho Gia Đình ( %d )",
		PerPerson: "Mỗi Người :", ClothingItems: "( %d ) Bộ Quần Áo", PantsLimit: "Giới Hạn ( %d ) Quần",
		ShoeSingular: "Đôi Giày", ShoePlural: "Đôi Giày",
		AccessorySingular: "Phụ Kiện", AccessoryPlural: "Phụ Kiện",
		SeasonalSingular: "Đồ Theo Mùa", SeasonalPlural: "Đồ Theo Mùa",
		Guest: "Khách ( %d )", GuestsLineSingular: "Khách ( %d )", GuestsLinePlural: "Khách ( %d )",
	},

	// --- NotoSansArabic (RTL; see shaping caveat above) ---------------------
	"ar": {
		Font: "NotoSansArabic", RTL: true,
		FamilySize: "حجم العائلة ( %d )", TotalItems: "إجمالي الملابس للعائلة ( %d )",
		PerPerson: "لكل شخص :", ClothingItems: "( %d ) قطعة ملابس", PantsLimit: "الحد ( %d ) سراويل",
		ShoeSingular: "زوج أحذية", ShoePlural: "أزواج أحذية",
		AccessorySingular: "إكسسوار", AccessoryPlural: "إكسسوارات",
		SeasonalSingular: "منتج موسمي", SeasonalPlural: "منتجات موسمية",
		Guest: "ضيف ( %d )", GuestsLineSingular: "ضيف ( %d )", GuestsLinePlural: "ضيوف ( %d )",
	},
	"fa": {
		Font: "NotoSansArabic", RTL: true,
		FamilySize: "اندازه خانواده ( %d )", TotalItems: "مجموع لباس برای خانواده ( %d )",
		PerPerson: "برای هر نفر :", ClothingItems: "( %d ) عدد لباس", PantsLimit: "محدودیت ( %d ) شلوار",
		ShoeSingular: "جفت کفش", ShoePlural: "جفت کفش",
		AccessorySingular: "لوازم جانبی", AccessoryPlural: "لوازم جانبی",
		SeasonalSingular: "کالای فصلی", SeasonalPlural: "کالاهای فصلی",
		Guest: "مهمان ( %d )", GuestsLineSingular: "مهمان ( %d )", GuestsLinePlural: "مهمانان ( %d )",
	},
	"ur": {
		Font: "NotoSansArabic", RTL: true,
		FamilySize: "خاندان کا حجم ( %d )", TotalItems: "خاندان کے لیے کل کپڑے ( %d )",
		PerPerson: "فی فرد :", ClothingItems: "( %d ) کپڑے", PantsLimit: "حد ( %d ) پتلونیں",
		ShoeSingular: "جوتوں کا جوڑا", ShoePlural: "جوتوں کے جوڑے",
		AccessorySingular: "لوازمات", AccessoryPlural: "لوازمات",
		SeasonalSingular: "موسمی شے", SeasonalPlural: "موسمی اشیاء",
		Guest: "مہمان ( %d )", GuestsLineSingular: "مہمان ( %d )", GuestsLinePlural: "مہمان ( %d )",
	},

	// --- NotoSansDevanagari -------------------------------------------------
	"hi": {
		Font: "NotoSansDevanagari",
		FamilySize: "परिवार का आकार ( %d )", TotalItems: "परिवार के लिए कुल कपड़े ( %d )",
		PerPerson: "प्रति व्यक्ति :", ClothingItems: "( %d ) कपड़े", PantsLimit: "सीमा ( %d ) पैंट",
		ShoeSingular: "जूतों की जोड़ी", ShoePlural: "जूतों के जोड़े",
		AccessorySingular: "सहायक वस्तु", AccessoryPlural: "सहायक वस्तुएँ",
		SeasonalSingular: "मौसमी वस्तु", SeasonalPlural: "मौसमी वस्तुएँ",
		Guest: "अतिथि ( %d )", GuestsLineSingular: "अतिथि ( %d )", GuestsLinePlural: "अतिथि ( %d )",
	},
}

// stringsFor resolves the localized strings for a code, falling back to English.
func stringsFor(code string) TicketStrings {
	if t, ok := translations[code]; ok {
		return t
	}
	return translations["en"]
}

// Language is a selectable ticket language (code + native display name).
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// langOrder fixes the display order of the language pickers. Add a code here and
// a translations entry above to support another language. Names read "English
// (Native)" so an English-speaking volunteer can scan the list while the guest
// still recognizes their own language; English needs no parenthetical.
var langOrder = []Language{
	{"en", "English"},
	{"es", "Spanish (Español)"},
	{"fr", "French (Français)"},
	{"de", "German (Deutsch)"},
	{"pt", "Portuguese (Português)"},
	{"it", "Italian (Italiano)"},
	{"nl", "Dutch (Nederlands)"},
	{"pl", "Polish (Polski)"},
	{"ro", "Romanian (Română)"},
	{"tr", "Turkish (Türkçe)"},
	{"sw", "Swahili (Kiswahili)"},
	{"ht", "Haitian Creole (Kreyòl Ayisyen)"},
	{"ru", "Russian (Русский)"},
	{"uk", "Ukrainian (Українська)"},
	{"el", "Greek (Ελληνικά)"},
	{"vi", "Vietnamese (Tiếng Việt)"},
	{"ar", "Arabic (العربية)"},
	{"fa", "Persian (فارسی)"},
	{"ur", "Urdu (اردو)"},
	{"hi", "Hindi (हिन्दी)"},
}

// Languages lists the supported ticket languages for the admin UI, in a stable
// order. Add a translations entry + a langOrder row to support another language.
func Languages() []Language {
	out := make([]Language, len(langOrder))
	copy(out, langOrder)
	return out
}

// LanguageInfo is one language's display name plus the exact ticket strings the
// printer uses, so the live preview can mirror the printed ticket precisely.
type LanguageInfo struct {
	Code    string        `json:"code"`
	Name    string        `json:"name"`
	RTL     bool          `json:"rtl"`
	Strings TicketStrings `json:"strings"`
}

// LanguageInfos returns, in display order, every language with its native name
// and ticket strings — the single source of truth shared by print and preview.
func LanguageInfos() []LanguageInfo {
	out := make([]LanguageInfo, 0, len(langOrder))
	for _, l := range langOrder {
		ts := stringsFor(l.Code)
		out = append(out, LanguageInfo{Code: l.Code, Name: l.Name, RTL: ts.RTL, Strings: ts})
	}
	return out
}
