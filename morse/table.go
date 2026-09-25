package morse

import (
	"regexp"
	"strings"
	"yama/config"
)

// MorseRegex now uses explicit Unicode hex escapes (\x{XXXX}) to guarantee 
// proper compilation of European and Esperanto characters regardless of file encoding.
var MorseRegex = regexp.MustCompile(`[^A-Z0-9\.\,\?\/\:\;\=\+\-\"\@\<\>\s\!\$\(\)\'\x{00C4}\x{00D6}\x{00DC}\x{00C9}\x{00C1}\x{00C5}\x{00C7}\x{00D1}\x{00C0}\x{00C8}\x{0108}\x{011C}\x{0124}\x{0134}\x{015C}\x{016C}]`)

// MorseTable is the dynamic "Source of Truth"
var MorseTable = make(map[rune]string)
var ProSignTable = make(map[string]string)

// ProSignTable remains static
var baseProSignMap = map[string]string{
	"AR": ".-.-.", "AS": ".-...", "BT": "-...-", "KA": "-.-.-",
	"SK": "...-.-", "VA": "...-.-", "VE": "...-.", "SN": "...-.",
	"BK": "-... -.-", "HH": "........", "DU": "-....-",
	"SOS": "...---...", "CH": "----",
}

var basicMap = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.",
	'G': "--.", 'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.",
	'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
	// Standard Punctuation
	'.': ".-.-.-",
	',': "--..--",
	'?': "..--..",
	'/': "-..-.",
	// ProSign Equivalents (MUST be in basic to prevent discarding)
	'-':      "-....-", // <DU>
	'\u2013': "-....-", // en dash <DU>
	'\u2014': "-....-", // em dash <DU>
}

var extendedPunctuationMap = map[rune]string{
	':':  "---...",
	';':  "-.-.-.",
	'"':  ".-..-.",
	'@':  ".--.-.",
	'\'': ".----.",
	'!':  "..--.",
	'$':  "...-..-",
	'(':  "-.--.",
	')':  "-.--.-",
}

var europeanMap = map[rune]string{
	'\u00C4': ".-.-",  // Ä A-umlaut
	'\u00D6': "---.",  // Ö O-umlaut
	'\u00DC': "..--",  // Ü U-umlaut
	'\u00C9': "..-..", // É E-acute
	'\u00C1': ".--.-", // Á A-acute
	'\u00C5': ".--.-", // Å A-ring
	'\u00C7': "-.-..", // Ç C-cedilla
	'\u00D1': "--.--", // Ñ N-tilde
	'\u00C0': ".--.-", // À A-grave
	'\u00C8': ".-..-", // È E-grave
}

// Add the new Esperanto map using strict Unicode points
var esperantoMap = map[rune]string{
	'\u0108': "-.-..", // Ĉ C-circumflex
	'\u011C': "--.-.", // Ĝ G-circumflex
	'\u0124': "----",  // Ĥ H-circumflex
	'\u0134': ".---.", // Ĵ J-circumflex
	'\u015C': "...-.", // Ŝ S-circumflex
	'\u016C': "..--",  // Ŭ U-breve
}

// Signature remains the same, we just bundle Esperanto into the European toggle
func RebuildMorseTable(useExtended bool, useEuropeanChars bool, useSkip bool, skipList string, euroSkipList string) {
	// 1. Wipe the working copies completely clean
	MorseTable = make(map[rune]string)
	ProSignTable = make(map[string]string)

	// 2. Rebuild the working copies from the blueprints
	for k, v := range basicMap { // basic english
		MorseTable[k] = v
	}
	if useExtended { // punc beyong ,.?/
		for k, v := range extendedPunctuationMap {
			MorseTable[k] = v
		}
	}

	if useEuropeanChars { // European and Esperanto
		// Load European
		for k, v := range europeanMap {
			MorseTable[k] = v
		}
		// Load Esperanto
		for k, v := range esperantoMap {
			MorseTable[k] = v
		}

		// Independent filter: Delete any that the user checked in the Euro/Esp picker
		if euroSkipList != "" {
			skipUpper := strings.ToUpper(euroSkipList)
			for _, r := range skipUpper {
				delete(MorseTable, r)
			}
		}
	}

	// === THE MASTER PROSIGN SWITCH ===
	if config.User.Playprosigns {
		for k, v := range baseProSignMap {
			ProSignTable[k] = v
		}
	}
	// =================================

	// 4. Apply the MAIN Skip List
	if useSkip && len(skipList) > 0 {
		tokens := Tokenize(strings.ToUpper(skipList))
		for _, t := range tokens {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				lookup := t[1 : len(t)-1]
				delete(ProSignTable, lookup)
			} else {
				if len(t) > 0 {
					r := []rune(t)[0]
					delete(MorseTable, r)
				}
			}
		}
	}
}

func ProcessMorseString(input string) string {
	// 2. Initial clean: Keep the original regex as a safety net for weird unicode
	work := MorseRegex.ReplaceAllString(input, "")

	// 3. Break into words first to naturally preserve our spaces
	words := strings.Fields(work)
	var cleanWords []string

	for _, w := range words {
		var cleanWordBuilder strings.Builder

		// 4. Token-Aware Filtering on each individual word
		tokens := Tokenize(w)
		for _, t := range tokens {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				// Prosign Check: Does it survive the working copy?
				lookup := t[1 : len(t)-1]
				if _, ok := ProSignTable[lookup]; ok {
					cleanWordBuilder.WriteString(t)
				}
			} else if len(t) > 0 {
				// Standard Character Check: Does it survive the working copy?
				r := []rune(t)[0]
				if _, ok := MorseTable[r]; ok {
					cleanWordBuilder.WriteString(t)
				}
			}
		}

		// 5. Only keep the word if it isn't empty after filtering
		cleanWord := cleanWordBuilder.String()
		if len(cleanWord) > 0 {
			cleanWords = append(cleanWords, cleanWord)
		}
	}

	// 6. SPACE CONDENSER
	return strings.Join(cleanWords, " ")
}

var syllables = map[string]string{
	"ABOUT": "A BOUT", "ABOVE": "A BOVE", "AFTER": "AF TER", "AGAIN": "A GAIN",
	"AGAINST": "A GAINST", "ALMOST": "AL MOST", "ALONE": "A LONE",
	"ALONG": "A LONG", "ALREADY": "AL READ Y", "ALSO": "AL SO",
	"ALTHOUGH": "AL THOUGH", "ALWAYS": "AL WAYS", "AMERICAN": "A MER I CAN",
	"AMONG": "A MONG", "ANOTHER": "A NOTH ER", "ANSWER": "AN SWER",
	"ANYTHING": "AN Y THING", "AREA": "AR E A", "AROUND": "A ROUND",
	"AWAY": "A WAY", "BEAUTIFUL": "BEAU TI FUL", "BECAUSE": "BE CAUSE",
	"BECOME": "BE COME", "BEFORE": "BE FORE", "BEGAN": "BE GAN",
	"BEGIN": "BE GIN", "BEHIND": "BE HIND", "BELIEVE": "BE LIEVE",
	"BETTER": "BET TER", "BETWEEN": "BE TWEEN", "BODY": "BOD Y",
	"BOTTOM": "BOT TOM", "BUSY": "BUS Y", "CARRY": "CAR RY",
	"CENTER": "CEN TER", "CENTURY": "CEN TU RY", "CERTAIN": "CER TAIN",
	"CHILDREN": "CHIL DREN", "CITY": "CIT Y", "COMMON": "COM MON",
	"COMPANY": "COM PA NY", "COMPUTER": "COM PU TER",
	"CONDITION": "CON DI TION", "CONSIDER": "CON SID ER",
	"CONTINUE": "CON TIN UE", "CONTROL": "CON TROL", "COPY": "COP Y",
	"CORNER": "COR NER", "CORRECT": "COR RECT", "COUNTRY": "COUN TRY",
	"COVER": "COV ER", "DAILY": "DAI LY", "DECIDE": "DE CIDE",
	"DEGREE": "DE GREE", "DEVELOP": "DE VEL OP", "DICTIONARY": "DIC TIO NAR Y",
	"DIFFERENT": "DIF FER ENT", "DIFFICULT": "DIF FI CULT", "DIRECT": "DI RECT",
	"DIRECTION": "DI REC TION", "DISCOVER": "DIS COV ER",
	"DISTANCE": "DIS TANCE", "DIVIDE": "DI VIDE", "DOLLAR": "DOL LAR",
	"DURING": "DUR ING", "EARLY": "EAR LY", "EASY": "EA SY",
	"ELECTRIC": "E LEC TRIC", "ELEMENT": "EL E MENT", "ENERGY": "EN ER GY",
	"ENGINE": "EN GINE", "ENGLISH": "ENG LISH", "ENJOY": "EN JOY",
	"ENOUGH": "E NOUGH", "ENTER": "EN TER", "EQUAL": "E QUAL",
	"EQUATION": "E QUA TION", "ESPECIALLY": "ES PE CIAL LY",
	"ESTABLISH": "ES TAB LISH", "EVEN": "E VEN", "EVENING": "E VEN ING",
	"EVER": "EV ER", "EVERY": "EV ER Y", "EVERYBODY": "EV ER Y BOD Y",
	"EVERYONE": "EV ER Y ONE", "EVERYTHING": "EV ER Y THING",
	"EVIDENT": "EV I DENT", "EXAMPLE": "EX AM PLE", "EXCITE": "EX CITE",
	"EXERCISE": "EX ER CISE", "EXPERIENCE": "EX PE RI ENCE",
	"EXPERIMENT": "EX PER I MENT", "EXPLAIN": "EX PLAIN", "EXPRESS": "EX PRESS",
	"EXTEND": "EX TEND", "FACTORY": "FAC TO RY", "FAMILY": "FAM I LY",
	"FAMOUS": "FA MOUS", "FATHER": "FA THER", "FEDERAL": "FED ER AL",
	"FIGURE": "FIG URE", "FINAL": "FI NAL", "FINGER": "FIN GER",
	"FINISH": "FIN ISH", "FLOWER": "FLOW ER", "FOLLOW": "FOL LOW",
	"FOREIGN": "FOR EIGN", "FOREST": "FOR EST", "FORGET": "FOR GET",
	"FORWARD": "FOR WARD", "FRACTION": "FRAC TION", "FREQUENT": "FRE QUENT",
	"FUNNY": "FUN NY", "FURTHER": "FUR THER", "FUTURE": "FU TURE",
	"GARDEN": "GAR DEN", "GENERAL": "GEN ER AL", "GOVERNMENT": "GOV ERN MENT",
	"GOVERNOR": "GOV ER NOR", "HAPPEN": "HAP PEN", "HAPPY": "HAP PY",
	"HEAVY": "HEAV Y", "HELLO": "HEL LO", "HISTORY": "HIS TO RY",
	"HIMSELF": "HIM SELF", "HOWEVER": "HOW EV ER", "HUMAN": "HU MAN",
	"HUNDRED": "HUN DRED", "HURRY": "HUR RY", "HUSBAND": "HUS BAND",
	"IDEA": "I DE A", "IMAGE": "IM AGE", "IMAGINE": "I MAG INE",
	"IMPORTANT": "IM POR TANT", "INCLUDE": "IN CLUDE", "INCREASE": "IN CREASE",
	"INDICATE": "IN DI CATE", "INDUSTRY": "IN DUS TRY",
	"INFORMATION": "IN FOR MA TION", "INSECT": "IN SECT", "INSIDE": "IN SIDE",
	"INSTEAD": "IN STEAD", "INTEND": "IN TEND", "INTEREST": "IN TER EST",
	"INVENT": "IN VENT", "IRON": "I RON", "ISLAND": "IS LAND",
	"ITSELF": "IT SELF", "KNOWLEDGE": "KNOW LEDGE", "LADY": "LA DY",
	"LANGUAGE": "LAN GUAGE", "LATER": "LA TER", "LEADER": "LEAD ER",
	"LETTER": "LET TER", "LEVEL": "LEV EL", "LISTEN": "LIS TEN",
	"LITTLE": "LIT TLE", "LOCAL": "LO CAL", "LOCATE": "LO CATE",
	"MACHINE": "MA CHINE", "MAJOR": "MA JOR", "MANY": "MAN Y",
	"MARKET": "MAR KET", "MASTER": "MAS TER", "MATERIAL": "MA TE RI AL",
	"MATTER": "MAT TER", "MAYBE": "MAY BE", "MEASURE": "MEAS URE",
	"MELODY": "MEL O DY", "MEMBER": "MEM BER", "METAL": "MET AL",
	"METHOD": "METH OD", "MIDDLE": "MID DLE", "MILLION": "MIL LION",
	"MINUTE": "MIN UTE", "MODERN": "MOD ERN", "MOLECULE": "MOL E CULE",
	"MOMENT": "MO MENT", "MONEY": "MON EY", "MORNING": "MORN ING",
	"MOTHER": "MOTH ER", "MOTION": "MO TION", "MOUNTAIN": "MOUN TAIN",
	"MOVEMENT": "MOVE MENT", "MUSIC": "MU SIC", "MYSELF": "MY SELF",
	"NATION": "NA TION", "NATURAL": "NAT U RAL", "NATURE": "NA TURE",
	"NEARLY": "NEAR LY", "NECESSARY": "NEC ES SAR Y", "NEIGHBOR": "NEIGH BOR",
	"NEITHER": "NEI THER", "NEVER": "NEV ER", "NOBODY": "NO BOD Y",
	"NOTHING": "NOTH ING", "NOTICE": "NO TICE", "NUMBER": "NUM BER",
	"NUMERAL": "NU MER AL", "OBJECT": "OB JECT", "OBSERVE": "OB SERVE",
	"OBTAIN": "OB TAIN", "OCEAN": "O CEAN", "OFFER": "OF FER",
	"OFFICE": "OF FICE", "OFFICER": "OF FI CER", "OFFICIAL": "OF FI CIAL",
	"OFTEN": "OF TEN", "ONLY": "ON LY", "OPEN": "O PEN",
	"OPERATION": "OP ER A TION", "OPPOSITE": "OP PO SITE", "ORDER": "OR DER",
	"ORGAN": "OR GAN", "ORIGINAL": "O RIG I NAL", "OTHER": "OTH ER",
	"OUTSIDE": "OUT SIDE", "OXYGEN": "OX Y GEN", "PALACE": "PAL ACE",
	"PAPER": "PA PER", "PARAGRAPH": "PAR A GRAPH", "PARTICULAR": "PAR TIC U LAR",
	"PARTY": "PAR TY", "PATTERN": "PAT TERN", "PERFECT": "PER FECT",
	"PERHAPS": "PER HAPS", "PERIOD": "PE RI OD", "PERSON": "PER SON",
	"PICTURE": "PIC TURE", "PLANET": "PLAN ET", "PLENTY": "PLEN TY",
	"PLURAL": "PLU RAL", "POEM": "PO EM", "POET": "PO ET",
	"POLICE": "PO LICE", "POLICY": "POL I CY", "POPULAR": "POP U LAR",
	"POPULATION": "POP U LA TION", "POSITION": "PO SI TION",
	"POSSIBLE": "POS SI BLE", "POWDER": "POW DER", "POWER": "POW ER",
	"PRACTICE": "PRAC TICE", "PREPARE": "PRE PARE", "PRESENT": "PRES ENT",
	"PRESIDENT": "PRES I DENT", "PRETTY": "PRET TY", "PRINCIPAL": "PRIN CI PAL",
	"PRINCIPLE": "PRIN CI PLE", "PRISON": "PRIS ON", "PRIVATE": "PRI VATE",
	"PROBABLE": "PROB A BLE", "PROBLEM": "PROB LEM", "PROCESS": "PROC ESS",
	"PRODUCE": "PRO DUCE", "PRODUCT": "PRO DUCT",
	"PRODUCTION": "PRO DUC TION", "PROFESSOR": "PRO FES SOR",
	"PROGRAM": "PRO GRAM", "PROGRESS": "PRO GRESS", "PROMISE": "PROM ISE",
	"PROPER": "PROP ER", "PROPERTY": "PROP ER TY", "PROTECT": "PRO TECT",
	"PROVIDE": "PRO VIDE", "PUBLIC": "PUB LIC", "PUNISH": "PUN ISH",
	"PUPIL": "PU PIL", "PURPLE": "PUR PLE", "PURPOSE": "PUR POSE",
	"QUALITY": "QUAL I TY", "QUESTION": "QUES TION", "QUIET": "QUI ET",
	"RATHER": "RATH ER", "REALLY": "REAL LY", "REASON": "REA SON",
	"RECEIVE": "RE CEIVE", "RECORD": "REC ORD", "REGION": "RE GION",
	"RELATE": "RE LATE", "RELIGION": "RE LI GION", "REMAIN": "RE MAIN",
	"REMEMBER": "RE MEM BER", "REPEAT": "RE PEAT", "REPLY": "RE PLY",
	"REPORT": "RE PORT", "REPRESENT": "REP RE SENT", "REQUIRE": "RE QUIRE",
	"RESPECT": "RE SPECT", "RESULT": "RE SULT", "RETURN": "RE TURN",
	"RIVER": "RIV ER", "SAFETY": "SAFE TY", "SAILOR": "SAIL OR",
	"SCIENCE": "SCI ENCE", "SECOND": "SEC OND", "SECTION": "SEC TION",
	"SEGMENT": "SEG MENT", "SELECT": "SE LECT", "SENTENCE": "SEN TENCE",
	"SEPARATE": "SEP A RATE", "SETTLE": "SET TLE", "SEVEN": "SEV EN",
	"SEVERAL": "SEV ER AL", "SHADOW": "SHAD OW", "SHOULDER": "SHOUL DER",
	"SIGNAL": "SIG NAL", "SILENT": "SI LENT", "SILVER": "SIL VER",
	"SIMILAR": "SIM I LAR", "SIMPLE": "SIM PLE", "SISTER": "SIS TER",
	"SOLDIER": "SOL DIER", "SOLID": "SOL ID", "SOMEBODY": "SOME BOD Y",
	"SOMEONE": "SOME ONE", "SOMETHING": "SOME THING",
	"SOMETIMES": "SOME TIMES", "SORRY": "SOR RY", "SOUTHERN": "SOUTH ERN",
	"SPECIAL": "SPE CIAL", "SPIRIT": "SPIR IT", "SPOKEN": "SPO KEN",
	"STATION": "STA TION", "STUDENT": "STU DENT", "STUDY": "STUD Y",
	"SUBJECT": "SUB JECT", "SUBSTANCE": "SUB STANCE", "SUCCESS": "SUC CESS",
	"SUDDEN": "SUD DEN", "SUFFIX": "SUF FIX", "SUGGEST": "SUG GEST",
	"SUMMER": "SUM MER", "SUPPLY": "SUP PLY", "SUPPORT": "SUP PORT",
	"SUPPOSE": "SUP POSE", "SURPRISE": "SUR PRISE", "SYLLABLE": "SYL LA BLE",
	"SYMBOL": "SYM BOL", "SYSTEM": "SYS TEM", "TABLE": "TA BLE",
	"TEMPERATURE": "TEM PER A TURE", "TERRIBLE": "TER RI BLE",
	"THEMSELVES": "THEM SELVES", "THEREFORE": "THERE FORE",
	"THICKNESS": "THICK NESS", "THIRTY": "THIR TY", "THOUSAND": "THOU SAND",
	"TICKET": "TICK ET", "TINY": "TI NY", "TODAY": "TO DAY",
	"TOGETHER": "TO GETH ER", "TOMORROW": "TO MOR ROW", "TONIGHT": "TO NIGHT",
	"TOTAL": "TO TAL", "TOWARD": "TO WARD", "TRAVEL": "TRAV EL",
	"TRIANGLE": "TRI AN GLE", "TROUBLE": "TROU BLE", "TWENTY": "TWEN TY",
	"UMBRELLA": "UM BREL LA", "UNDER": "UN DER",
	"UNDERSTAND": "UN DER STAND", "UNIT": "U NIT", "UNTIL": "UN TIL",
	"UPON": "U PON", "USUAL": "U SUAL", "VALLEY": "VAL LEY",
	"VALUE": "VAL UE", "VELOCITY": "VE LOC I TY", "VILLAGE": "VIL LAGE",
	"VISIT": "VIS IT", "VOLUME": "VOL UME", "VOWEL": "VOW EL",
	"WAGON": "WAG ON", "WATER": "WA TER", "WEATHER": "WEATH ER",
	"WELCOME": "WEL COME", "WESTERN": "WEST ERN", "WHATEVER": "WHAT EV ER",
	"WHETHER": "WHETH ER", "WINDOW": "WIN DOW", "WINTER": "WIN TER",
	"WITHIN": "WITH IN", "WITHOUT": "WITH OUT", "WOMAN": "WOM AN",
	"WOMEN": "WOM EN", "WONDER": "WON DER", "WORKER": "WORK ER",
	"WRITTEN": "WRIT TEN", "YELLOW": "YEL LOW",
}


var EchoMapBase = map[string]string{
	".-":   "A", "-...": "B", "-.-.": "C", "-..":  "D", ".":    "E",
	"..-.": "F", "--.":  "G", "....": "H", "..":   "I", ".---": "J",
	"-.-":  "K", ".-..": "L", "--":   "M", "-.":   "N", "---":  "O",
	".--.": "P", "--.-": "Q", ".-.":  "R", "...":  "S", "-":    "T",
	"..-":  "U", "...-": "V", ".--":  "W", "-..-": "X", "-.--": "Y",
	"--..": "Z",
	"-----": "0", ".----": "1", "..---": "2", "...--": "3", "....-": "4",
	".....": "5", "-....": "6", "--...": "7", "---..": "8", "----.": "9",
	".-.-.-": ".", "--..--": ",", "..--..": "?", "-..-.":  "/",
}

var EchoMapExtended = map[string]string{
	"---...":  ":", "-.-.-.":  ";", ".-..-.":  "\"", ".--.-.":  "@",
	".----.":  "'", "..--.":   "!", "...-..-": "$", "-.--.":   "(", "-.--.-":  ")",
}

var EchoMapProsigns = map[string]string{
	".-...":     "<AS>", "-...-":     "<BT>", ".-.-.":     "<AR>",
	"-....-":    "<DU>", "-.-.-":     "<KA>", "...-.-":    "<SK>", 
	"........":  "<HH>", "...---...": "<SOS>", "----":      "<CH>",
	"...-.":     "<VE>", "-...-.-":   "<BK>",
}

// EchoMapEuropean fixed to output true UTF-8 strings instead of mojibake
var EchoMapEuropean = map[string]string{
	".-.-":  "\u00C4", // Ä
	"---.":  "\u00D6", // Ö
	"..--":  "\u00DC", // Ü
	"..-..": "\u00C9", // É
	".--.-": "\u00C1", // Á (also Å, À)
	"-.-..": "\u00C7", // Ç
	"--.--": "\u00D1", // Ñ
	".-..-": "\u00C8", // È
}

var EchoMap map[string]string // to be populated
