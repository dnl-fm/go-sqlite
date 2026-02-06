package query

import (
	"sort"
	"unicode"
)

// ExtractParams returns all unique parameter names found in the SQL string.
// Parameters use :name syntax.
//
// Example:
//
//	params := ExtractParams("SELECT * FROM users WHERE id = :id AND name = :name")
//	// returns: ["id", "name"] (sorted)
func ExtractParams(sqlStr string) []string {
	matches := paramPattern.FindAllStringSubmatch(sqlStr, -1)

	// Use map to deduplicate (same param can appear multiple times)
	paramSet := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			paramSet[match[1]] = true
		}
	}

	// Convert to sorted slice for consistent output
	params := make([]string, 0, len(paramSet))
	for name := range paramSet {
		params = append(params, name)
	}
	sort.Strings(params)

	return params
}

// IsValidParamName checks if a parameter name is valid.
// Valid names: start with letter or underscore, followed by alphanumeric or underscore.
func IsValidParamName(name string) bool {
	if name == "" {
		return false
	}

	// Check first character
	first := rune(name[0])
	if !unicode.IsLetter(first) && first != '_' {
		return false
	}

	// Check remaining characters
	for _, ch := range name[1:] {
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' {
			return false
		}
	}

	return true
}
