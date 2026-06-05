package query

// isNextCompareToString returns true when the identifier at position i is followed
// by = / != / <> and then a string literal or another identifier.
// Used to add ::text on PostgreSQL to prevent "operator does not exist: uuid = text".
func isNextCompareToString(tokens []tok, i int) bool {
	// Look ahead: skip dots (qualified names like Д.Номенклатура)
	j := i + 1
	for j < len(tokens) && tokens[j].kind == tDot {
		j += 2 // skip dot and the following ident
	}
	if j >= len(tokens) {
		return false
	}
	// Check for = / != / <>
	if tokens[j].kind != tOp {
		return false
	}
	op := tokens[j].val
	if op != "=" && op != "!=" && op != "<>" {
		return false
	}
	j++
	if j >= len(tokens) {
		return false
	}
	// Right side: string literal or identifier (column name)
	return tokens[j].kind == tStr || tokens[j].kind == tIdent
}
