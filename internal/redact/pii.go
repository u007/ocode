package redact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/u007/dataprotect/analyzer"
	"github.com/u007/dataprotect/recognizers"
	"github.com/u007/dataprotect/recognizers/checksum"
	"github.com/u007/dataprotect/recognizers/finance"
	"github.com/u007/dataprotect/recognizers/identity"
	"github.com/u007/dataprotect/recognizers/legal"
	"github.com/u007/dataprotect/recognizers/national"
	"github.com/u007/dataprotect/recognizers/regex"
)

// PIICategory identifies one opt-in group of PII recognizers. The names are the
// values stored in the config file.
type PIICategory string

const (
	PIIContact   PIICategory = "contact"
	PIIFinancial PIICategory = "financial"
	PIIIdentity  PIICategory = "identity"
	PIILegal     PIICategory = "legal"
	PIINetwork   PIICategory = "network"
	PIIMoney     PIICategory = "money"
)

// AllPIICategories lists every group in a stable order.
var AllPIICategories = []PIICategory{
	PIIContact,
	PIIFinancial,
	PIIIdentity,
	PIILegal,
	PIINetwork,
	PIIMoney,
}

// ParsePIICategories converts configured names to categories. An unknown name
// or a duplicate is an error, mirroring config.ValidatePIICategories.
func ParsePIICategories(names []string) ([]PIICategory, error) {
	cats := make([]PIICategory, 0, len(names))
	seen := make(map[PIICategory]bool, len(names))
	for _, name := range names {
		cat := PIICategory(name)
		if !isPIICategory(cat) {
			return nil, fmt.Errorf("redact: unknown PII category %q", name)
		}
		if seen[cat] {
			return nil, fmt.Errorf("redact: duplicate PII category %q", name)
		}
		seen[cat] = true
		cats = append(cats, cat)
	}
	return cats, nil
}

func isPIICategory(cat PIICategory) bool {
	for _, c := range AllPIICategories {
		if c == cat {
			return true
		}
	}
	return false
}

// recognizersFor returns the recognizers wired to one group. The mapping is the
// approved design's table; recognizers for disabled groups are never built.
func recognizersFor(cat PIICategory) []recognizers.Recognizer {
	switch cat {
	case PIIContact:
		return []recognizers.Recognizer{
			regex.NewEmailRecognizer(),
			regex.NewPhoneRecognizer(),
		}
	case PIIFinancial:
		return []recognizers.Recognizer{
			checksum.NewLuhnValidator(),
			checksum.NewIBANValidator(),
			finance.NewSWIFTBICRecognizer(),
			finance.NewUSRoutingRecognizer(),
			finance.NewCardCVVRecognizer(),
			finance.NewBankAccountRecognizer(),
			finance.NewPINRecognizer(),
			finance.NewLEIRecognizer(),
			finance.NewISINRecognizer(),
			finance.NewEthereumRecognizer(),
			regex.NewBitcoinRecognizer(),
		}
	case PIIIdentity:
		return []recognizers.Recognizer{
			regex.NewSSNRecognizer(),
			identity.NewPassportRecognizer(),
			identity.NewDriverLicenseRecognizer(),
			identity.NewTaxIDRecognizer(),
			identity.NewDateOfBirthRecognizer(),
			national.NewSGNRICRecognizer(),
			national.NewMYICRecognizer(),
			national.NewTHIDRecognizer(),
			national.NewPHTINRecognizer(),
			national.NewINGSTINRecognizer(),
			national.NewINPANRecognizer(),
			national.NewSGUENRecognizer(),
			national.NewMYSSMRecognizer(),
			national.NewMYTINRecognizer(),
			national.NewMYPassportRecognizer(),
			national.NewSGPassportRecognizer(),
		}
	case PIILegal:
		return []recognizers.Recognizer{
			legal.NewCaseNumberRecognizer(),
		}
	case PIINetwork:
		return []recognizers.Recognizer{
			regex.NewIPRecognizer(),
			regex.NewURLRecognizer(),
		}
	case PIIMoney:
		return []recognizers.Recognizer{
			finance.NewMoneyRecognizer(),
		}
	}
	return nil
}

func canonicalCategories(cats []PIICategory) []PIICategory {
	seen := make(map[PIICategory]bool, len(cats))
	out := make([]PIICategory, 0, len(cats))
	for _, c := range cats {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func canonicalCategoryKey(cats []PIICategory) string {
	canon := canonicalCategories(cats)
	parts := make([]string, len(canon))
	for i, c := range canon {
		parts[i] = string(c)
	}
	return strings.Join(parts, ",")
}

func canonicalWordsKey(words []string) string {
	if len(words) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(words))
	uniq := make([]string, 0, len(words))
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			uniq = append(uniq, w)
		}
	}
	sort.Strings(uniq)
	return strings.Join(uniq, "\x1f")
}

// analyzerCache holds one Analyzer per canonical category set. Analyze does not
// write to the receiver, so a shared instance is safe for concurrent use.
var analyzerCache sync.Map // map[string]*analyzer.Analyzer

func analyzerFor(cats []PIICategory) *analyzer.Analyzer {
	key := canonicalCategoryKey(cats)
	if cached, ok := analyzerCache.Load(key); ok {
		return cached.(*analyzer.Analyzer)
	}
	a := analyzer.New(analyzer.WithMinConfidence(0.7))
	for _, cat := range canonicalCategories(cats) {
		for _, r := range recognizersFor(cat) {
			a.AddRecognizer(r)
		}
	}
	actual, _ := analyzerCache.LoadOrStore(key, a)
	return actual.(*analyzer.Analyzer)
}

// piiAnalyzerFor is the provider seam: tests replace it to inject a failing or
// counting analyzer.
var piiAnalyzerFor = analyzerFor

const (
	piiKindPrefix = "pii:"
	// piiMemoGenerationSize bounds each memo generation. Two generations are
	// live (512 entries total), so an entry survives exactly one rotation.
	piiMemoGenerationSize = 256
)

// piiMemo is a process-wide cache of PII detections keyed by the full input
// fingerprint. It stores only offsets and kinds, never matched text. Two
// generations are kept: a put that fills "current" rotates it into "previous",
// so recently used keys survive one rotation instead of a full clear.
var piiMemo = struct {
	mu       sync.Mutex
	current  map[string][]Span
	previous map[string][]Span
}{
	current:  make(map[string][]Span),
	previous: make(map[string][]Span),
}

func piiMemoKey(text string, cats []PIICategory, customWords []string, fileContent bool) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:]) + "|" +
		canonicalCategoryKey(cats) + "|" +
		canonicalWordsKey(customWords) + "|" +
		strconv.FormatBool(fileContent)
}

func piiMemoGet(key string) ([]Span, bool) {
	piiMemo.mu.Lock()
	defer piiMemo.mu.Unlock()
	if spans, ok := piiMemo.current[key]; ok {
		return append([]Span(nil), spans...), true
	}
	if spans, ok := piiMemo.previous[key]; ok {
		return append([]Span(nil), spans...), true
	}
	return nil, false
}

func piiMemoPut(key string, spans []Span) {
	piiMemo.mu.Lock()
	defer piiMemo.mu.Unlock()
	if piiMemo.current == nil {
		piiMemo.current = make(map[string][]Span)
	}
	if len(piiMemo.current) >= piiMemoGenerationSize {
		piiMemo.previous = piiMemo.current
		piiMemo.current = make(map[string][]Span)
	}
	piiMemo.current[key] = append([]Span(nil), spans...)
}

// piiEntityGroups maps every entity type wired in recognizersFor to its
// group. A recognizer added without an entry here cannot have a confidence
// floor, so TestPIIEntityGroupsCoverRecognizers fails loudly rather than
// letting the new type bypass group filtering.
var piiEntityGroups = map[recognizers.EntityType]PIICategory{
	// contact
	recognizers.EntityEmail: PIIContact,
	recognizers.EntityPhone: PIIContact,

	// financial
	recognizers.EntityCreditCard:  PIIFinancial,
	recognizers.EntityIBAN:        PIIFinancial,
	recognizers.EntitySWIFTBIC:    PIIFinancial,
	recognizers.EntityUSRouting:   PIIFinancial,
	recognizers.EntityCardCVV:     PIIFinancial,
	recognizers.EntityBankAccount: PIIFinancial,
	recognizers.EntityPIN:         PIIFinancial,
	recognizers.EntityLEI:         PIIFinancial,
	recognizers.EntityISIN:        PIIFinancial,
	recognizers.EntityEthereum:    PIIFinancial,
	recognizers.EntityBitcoin:     PIIFinancial,

	// identity
	recognizers.EntitySSN:           PIIIdentity,
	recognizers.EntityPassport:      PIIIdentity,
	recognizers.EntityDriverLicense: PIIIdentity,
	recognizers.EntityTaxID:         PIIIdentity,
	recognizers.EntityDateOfBirth:   PIIIdentity,
	recognizers.EntitySGNRIC:        PIIIdentity,
	recognizers.EntityMYIC:          PIIIdentity,
	recognizers.EntityTHID:          PIIIdentity,
	recognizers.EntityPHTIN:         PIIIdentity,
	recognizers.EntityINGSTIN:       PIIIdentity,
	recognizers.EntityINPAN:         PIIIdentity,
	recognizers.EntitySGUEN:         PIIIdentity,
	recognizers.EntityMYSSM:         PIIIdentity,
	recognizers.EntityMYTIN:         PIIIdentity,
	recognizers.EntityMYPassport:    PIIIdentity,
	recognizers.EntitySGPassport:    PIIIdentity,

	// legal
	recognizers.EntityCaseNumber: PIILegal,

	// network
	recognizers.EntityIP:  PIINetwork,
	recognizers.EntityURL: PIINetwork,

	// money
	recognizers.EntityMoney: PIIMoney,
}

// piiGroupFloor is the confidence an entity must reach to stay in its group.
// network and money are the highest-noise groups, so they demand more than the
// analyzer's own 0.7 floor; every other group keeps that floor.
func piiGroupFloor(cat PIICategory) float64 {
	switch cat {
	case PIINetwork:
		return 0.90
	case PIIMoney:
		return 0.85
	}
	return 0.7
}

// detectPII runs the PII analyzer for the given category set and returns spans
// with a "pii:" kind prefix. Token overlaps are skipped; a recognizer panic is
// converted to an error.
func detectPII(text string, cats []PIICategory, customWords []string, fileContent bool) (spans []Span, err error) {
	if len(cats) == 0 {
		return nil, nil
	}

	key := piiMemoKey(text, cats, customWords, fileContent)
	if cached, ok := piiMemoGet(key); ok {
		return cached, nil
	}

	defer func() {
		if r := recover(); r != nil {
			spans = nil
			err = fmt.Errorf("redact: PII detection panicked: %v", r)
		}
	}()

	entities, analyzeErr := piiAnalyzerFor(cats).Analyze(context.Background(), text)
	if analyzeErr != nil {
		return nil, fmt.Errorf("redact: PII detection failed: %w", analyzeErr)
	}

	tokens := TokenPattern.FindAllStringIndex(text, -1)
	out := make([]Span, 0, len(entities))
	for _, e := range entities {
		if e.Start < 0 || e.End > len(text) || e.Start >= e.End {
			continue
		}
		// Drop an entity that does not reach its group's floor. An unmapped
		// type has no floor to apply and is kept; the map completeness test is
		// what catches a recognizer wired without a group.
		if group, ok := piiEntityGroups[e.Type]; ok && e.Confidence < piiGroupFloor(group) {
			continue
		}
		s := Span{Start: e.Start, End: e.End, Kind: piiKindPrefix + string(e.Type)}
		if overlapsRanges(s, tokens) {
			continue
		}
		out = append(out, s)
	}

	piiMemoPut(key, out)
	return append([]Span(nil), out...), nil
}

// overlapsRanges reports whether s overlaps any [start,end) pair.
func overlapsRanges(s Span, ranges [][]int) bool {
	for _, r := range ranges {
		if s.Start < r[1] && r[0] < s.End {
			return true
		}
	}
	return false
}
