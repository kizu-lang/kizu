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
