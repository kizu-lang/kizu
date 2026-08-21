package typ

// Table retains parsed type trees for one compiler phase. Type interface values
// act as immutable pointer handles into these trees, and Parse reuses a retained
// root when the same canonical spelling is queried again.
type Table struct {
	parsed map[string]Type
}

// NewTable creates an empty type table.
func NewTable() *Table {
	return &Table{parsed: map[string]Type{}}
}

// Remember retains value and registers its nested types by canonical spelling.
func (t *Table) Remember(value Type) Type {
	if value == nil {
		return nil
	}
	name := value.String()
	if parsed, ok := t.parsed[name]; ok {
		return parsed
	}
	Walk(value, func(node Type) {
		nodeName := node.String()
		if _, exists := t.parsed[nodeName]; !exists {
			t.parsed[nodeName] = node
		}
	})
	return value
}

// Parse returns the retained structure for text, parsing it only on its first
// use in this table.
func (t *Table) Parse(text string) (Type, error) {
	if parsed, ok := t.parsed[text]; ok {
		return parsed, nil
	}
	parsed, err := Parse(text)
	if err != nil {
		return nil, err
	}
	return t.Remember(parsed), nil
}
