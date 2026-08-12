package cstr

import (
	"testing"
	"unsafe"
)

func TestString(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		limit int
		want  string
	}{
		{name: "nil", limit: 8},
		{name: "zero limit", data: []byte("text\x00")},
		{name: "terminator", data: []byte("text\x00ignored"), limit: 16, want: "text"},
		{name: "limit", data: []byte("longer\x00"), limit: 4, want: "long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ptr unsafe.Pointer
			if tt.data != nil {
				ptr = unsafe.Pointer(&tt.data[0])
			}
			if got := String(ptr, tt.limit); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStringCopiesNativeBytes(t *testing.T) {
	data := []byte("text\x00")
	got := String(unsafe.Pointer(&data[0]), len(data))
	data[0] = 'T'
	if got != "text" {
		t.Fatalf("String() retained native storage: %q", got)
	}
}
