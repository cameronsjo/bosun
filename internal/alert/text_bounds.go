package alert

import "strings"

const truncationSuffix = "..."

// truncateByUnits retains a prefix within maxUnits and includes the suffix in
// the bound. A component that cannot fit the complete suffix is omitted.
func truncateByUnits(text string, maxUnits int, runeUnits func(rune) int) string {
	if textUnits(text, runeUnits) <= maxUnits {
		return text
	}

	suffixUnits := textUnits(truncationSuffix, runeUnits)
	if maxUnits < suffixUnits {
		return ""
	}

	prefixBudget := maxUnits - suffixUnits
	var prefix strings.Builder
	used := 0
	for _, r := range text {
		units := runeUnits(r)
		if used+units > prefixBudget {
			break
		}
		prefix.WriteRune(r)
		used += units
	}

	return prefix.String() + truncationSuffix
}

func textUnits(text string, runeUnits func(rune) int) int {
	total := 0
	for _, r := range text {
		total += runeUnits(r)
	}
	return total
}

func utf16RuneUnits(r rune) int {
	if r > 0xffff {
		return 2
	}
	return 1
}

func utf16Units(text string) int {
	return textUnits(text, utf16RuneUnits)
}

func truncateUTF16(text string, maxUnits int) string {
	return truncateByUnits(text, maxUnits, utf16RuneUnits)
}
