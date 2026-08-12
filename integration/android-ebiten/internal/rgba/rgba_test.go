package rgba

import (
	"bytes"
	"testing"
)

func TestPackRemovesRowPadding(t *testing.T) {
	data := []byte{
		1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0,
		9, 10, 11, 12, 13, 14, 15, 16, 0, 0, 0, 0,
	}

	got, err := Pack(data, 12, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	if !bytes.Equal(got, want) {
		t.Fatalf("Pack() = %v, want %v", got, want)
	}
}

func TestPackFlipsNegativeStride(t *testing.T) {
	data := []byte{
		9, 10, 11, 12,
		1, 2, 3, 4,
	}

	got, err := Pack(data, -4, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 2, 3, 4, 9, 10, 11, 12}
	if !bytes.Equal(got, want) {
		t.Fatalf("Pack() = %v, want %v", got, want)
	}
}

func TestPackRejectsShortBuffer(t *testing.T) {
	if _, err := Pack(make([]byte, 7), 8, 2, 1); err == nil {
		t.Fatal("Pack() error = nil, want invalid buffer error")
	}
}
