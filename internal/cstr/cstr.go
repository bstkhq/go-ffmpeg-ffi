// Package cstr contains bounded conversions for borrowed native C strings.
package cstr

import "unsafe"

// String copies a NUL-terminated native string into Go, scanning at most limit
// bytes. If no terminator is found, the returned string is truncated to limit.
func String(ptr unsafe.Pointer, limit int) string {
	if ptr == nil || limit <= 0 {
		return ""
	}

	length := 0
	for length < limit {
		if *(*byte)(unsafe.Add(ptr, length)) == 0 {
			break
		}
		length++
	}
	if length == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(ptr), length))
}
