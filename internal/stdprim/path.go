package stdprim

import "path"

// PathJoin joins slash-separated path fragments with host-independent rules.
func PathJoin(left string, right string) string {
	return path.Join(left, right)
}

// PathClean normalizes a slash-separated path.
func PathClean(value string) string {
	return path.Clean(value)
}

// PathBase returns the final slash-separated path element.
func PathBase(value string) string {
	return path.Base(value)
}

// PathDir returns all but the final slash-separated path element.
func PathDir(value string) string {
	return path.Dir(value)
}

// PathExt returns the final extension of a slash-separated path.
func PathExt(value string) string {
	return path.Ext(value)
}
