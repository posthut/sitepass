package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	plaintextPrefix = "pv_"
	randomBytes     = 32
	suffixLen       = 10
	slugMaxLen      = 24
)

var (
	labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}[a-z0-9]$`)
	reservedLabels = map[string]struct{}{
		"www": {}, "api": {}, "admin": {}, "mail": {},
		"static": {}, "assets": {}, "cdn": {}, "status": {},
	}
	base32Encoding = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// Generated holds a freshly minted plaintext token and its derived fields.
type Generated struct {
	Plaintext  string
	Hash       [32]byte
	Prefix     string
	Subdomain  string
}

// Generate creates a token and subdomain label. projectName may be empty.
func Generate(projectName string) (Generated, error) {
	raw := make([]byte, randomBytes)
	if _, err := rand.Read(raw); err != nil {
		return Generated{}, fmt.Errorf("read random bytes: %w", err)
	}
	encoded := strings.ToLower(base32Encoding.EncodeToString(raw))
	plaintext := plaintextPrefix + encoded
	hash := sha256.Sum256([]byte(plaintext))

	suffix := encoded
	if len(suffix) > suffixLen {
		suffix = suffix[:suffixLen]
	}
	slug := slugFromProjectName(projectName)
	subdomain := slug + "-" + suffix
	if !labelPattern.MatchString(subdomain) {
		return Generated{}, fmt.Errorf("derived subdomain %q failed validation", subdomain)
	}
	if _, reserved := reservedLabels[slug]; reserved {
		slug = twoWordLabel()
		subdomain = slug + "-" + suffix
	}

	prefix := plaintext
	if len(prefix) > 10 {
		prefix = prefix[:10]
	}
	return Generated{
		Plaintext: plaintext,
		Hash:      hash,
		Prefix:    prefix,
		Subdomain: subdomain,
	}, nil
}

// HashPlaintext returns the SHA-256 of a bearer token string.
func HashPlaintext(plaintext string) [32]byte {
	return sha256.Sum256([]byte(plaintext))
}

func slugFromProjectName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return twoWordLabel()
	}
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		r = transliterateRune(r)
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if b.Len() > 0 && !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return twoWordLabel()
	}
	if len(slug) > slugMaxLen {
		slug = strings.Trim(slug[:slugMaxLen], "-")
	}
	if slug == "" || len(slug) < 2 {
		return twoWordLabel()
	}
	if _, reserved := reservedLabels[slug]; reserved {
		return twoWordLabel()
	}
	return slug
}

func twoWordLabel() string {
	// Short pronounceable fallback; entropy remains in the random suffix.
	a := randomWord(adjectives)
	b := randomWord(nouns)
	return a + "-" + b
}

func randomWord(words []string) string {
	var buf [1]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return words[0]
	}
	return words[int(buf[0])%len(words)]
}

var adjectives = []string{
	"amber", "brisk", "calm", "clear", "crisp", "fair", "fresh", "gentle",
	"keen", "light", "lucid", "mild", "neat", "open", "plain", "quick",
	"rapid", "sharp", "solid", "swift", "tidy", "vivid", "warm", "wise",
}

var nouns = []string{
	"brook", "cedar", "cloud", "coast", "field", "fjord", "grove", "haven",
	"island", "lake", "maple", "meadow", "orbit", "pine", "ridge", "river",
	"shore", "stone", "trail", "vale", "wave", "wheat", "willow", "wind",
}

func transliterateRune(r rune) rune {
	if mapped, ok := cyrillicToLatin[r]; ok {
		return mapped
	}
	return r
}

// Minimal Cyrillic map covering Russian and Kazakh letters used in names.
var cyrillicToLatin = map[rune]rune{
	'а': 'a', 'б': 'b', 'в': 'v', 'г': 'g', 'д': 'd', 'е': 'e', 'ё': 'e',
	'ж': 'z', 'з': 'z', 'и': 'i', 'й': 'i', 'к': 'k', 'л': 'l', 'м': 'm',
	'н': 'n', 'о': 'o', 'п': 'p', 'р': 'r', 'с': 's', 'т': 't', 'у': 'u',
	'ф': 'f', 'х': 'h', 'ц': 'c', 'ч': 'c', 'ш': 's', 'щ': 's', 'ъ': '-',
	'ы': 'y', 'ь': '-', 'э': 'e', 'ю': 'u', 'я': 'a',
	'ә': 'a', 'ғ': 'g', 'қ': 'q', 'ң': 'n', 'ө': 'o', 'ұ': 'u', 'ү': 'u',
	'һ': 'h', 'і': 'i',
}
