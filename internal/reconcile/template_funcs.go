package reconcile

import (
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// deniedSprigFuncs are the Sprig functions that read the process environment
// or reach the network. Templates come from the GitOps repository and render
// inside the daemon, whose environment carries the Git token, API tokens, and
// often the age key, so these are never offered (cameronsjo/bosun#790).
var deniedSprigFuncs = []string{"env", "expandenv", "getHostByName"}

// allowedSprigFuncs is the Template Function Allowlist: every Sprig v3.3.0
// TxtFuncMap name except deniedSprigFuncs, sorted. A function a later Sprig
// release adds is not offered until it is reviewed into this list; the golden
// test in template_funcs_test.go fails when one appears.
var allowedSprigFuncs = []string{
	"abbrev", "abbrevboth", "add", "add1", "add1f", "addf",
	"adler32sum", "ago", "all", "any", "append", "atoi",
	"b32dec", "b32enc", "b64dec", "b64enc", "base", "bcrypt",
	"biggest", "buildCustomCert", "camelcase", "cat", "ceil", "chunk",
	"clean", "coalesce", "compact", "concat", "contains", "date",
	"dateInZone", "dateModify", "date_in_zone", "date_modify", "decryptAES", "deepCopy",
	"deepEqual", "default", "derivePassword", "dict", "dig", "dir",
	"div", "divf", "duration", "durationRound", "empty", "encryptAES",
	"ext", "fail", "first", "float64", "floor", "fromJson",
	"genCA", "genCAWithKey", "genPrivateKey", "genSelfSignedCert", "genSelfSignedCertWithKey", "genSignedCert",
	"genSignedCertWithKey", "get", "has", "hasKey", "hasPrefix", "hasSuffix",
	"hello", "htmlDate", "htmlDateInZone", "htpasswd", "indent", "initial",
	"initials", "int", "int64", "isAbs", "join", "kebabcase",
	"keys", "kindIs", "kindOf", "last", "list", "lower",
	"max", "maxf", "merge", "mergeOverwrite", "min", "minf",
	"mod", "mul", "mulf", "mustAppend", "mustChunk", "mustCompact",
	"mustDateModify", "mustDeepCopy", "mustFirst", "mustFromJson", "mustHas", "mustInitial",
	"mustLast", "mustMerge", "mustMergeOverwrite", "mustPrepend", "mustRegexFind", "mustRegexFindAll",
	"mustRegexMatch", "mustRegexReplaceAll", "mustRegexReplaceAllLiteral", "mustRegexSplit", "mustRest", "mustReverse",
	"mustSlice", "mustToDate", "mustToJson", "mustToPrettyJson", "mustToRawJson", "mustUniq",
	"mustWithout", "must_date_modify", "nindent", "nospace", "now", "omit",
	"osBase", "osClean", "osDir", "osExt", "osIsAbs", "pick",
	"pluck", "plural", "prepend", "quote", "randAlpha", "randAlphaNum",
	"randAscii", "randBytes", "randInt", "randNumeric", "regexFind", "regexFindAll",
	"regexMatch", "regexQuoteMeta", "regexReplaceAll", "regexReplaceAllLiteral", "regexSplit", "repeat",
	"replace", "rest", "reverse", "round", "semver", "semverCompare",
	"seq", "set", "sha1sum", "sha256sum", "sha512sum", "shuffle",
	"slice", "snakecase", "sortAlpha", "split", "splitList", "splitn",
	"squote", "sub", "subf", "substr", "swapcase", "ternary",
	"title", "toDate", "toDecimal", "toJson", "toPrettyJson", "toRawJson",
	"toString", "toStrings", "trim", "trimAll", "trimPrefix", "trimSuffix",
	"trimall", "trunc", "tuple", "typeIs", "typeIsLike", "typeOf",
	"uniq", "unixEpoch", "unset", "until", "untilStep", "untitle",
	"upper", "urlJoin", "urlParse", "uuidv4", "values", "without",
	"wrap", "wrapWith",
}

// SafeSprigFuncMap returns only the allowlisted Sprig functions, for reconcile
// templates and bosun render alike. A template that calls a function outside
// the allowlist fails to parse.
func SafeSprigFuncMap() template.FuncMap {
	all := sprig.TxtFuncMap()
	funcs := make(template.FuncMap, len(allowedSprigFuncs))
	for _, name := range allowedSprigFuncs {
		if fn, ok := all[name]; ok {
			funcs[name] = fn
		}
	}
	return funcs
}
