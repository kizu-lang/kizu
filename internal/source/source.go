// Package source owns compiler input text and gives syntax records stable source handles.
package source

// Map owns every source file loaded for one compiler invocation.
type Map struct {
	files []File
}

// File is one immutable compiler input.
type File struct {
	path string
	text string
}

// ID is a copyable handle to one file in a Map.
//
// Keeping the map reference inside the Go handle preserves the lifetime that
// the Kizu implementation carries explicitly around its integer source ID.
type ID struct {
	sources *Map
	index   int
}

// NewMap creates an empty source map.
func NewMap() *Map {
	return &Map{}
}

// Add stores one immutable source file and returns its stable handle.
func (m *Map) Add(path string, text string) ID {
	id := ID{sources: m, index: len(m.files)}
	m.files = append(m.files, File{path: path, text: text})
	return id
}

// Len returns the number of files owned by the map.
func (m *Map) Len() int {
	return len(m.files)
}

// IsZero reports whether id names no source file.
func (id ID) IsZero() bool {
	return id.sources == nil
}

// Path returns the path of the source file named by id.
func (id ID) Path() string {
	if file, ok := id.file(); ok {
		return file.path
	}
	return ""
}

// Text returns the immutable input text of the source file named by id.
func (id ID) Text() string {
	if file, ok := id.file(); ok {
		return file.text
	}
	return ""
}

// file resolves id only inside the map that created it.
func (id ID) file() (*File, bool) {
	if id.sources == nil || id.index < 0 || id.index >= len(id.sources.files) {
		return nil, false
	}
	return &id.sources.files[id.index], true
}
