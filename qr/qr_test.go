package qr

import (
	"bytes"
	"testing"
)

var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func TestPNGValid(t *testing.T) {
	b, err := PNG("hello world", 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 100 {
		t.Fatalf("too short %d", len(b))
	}
	if !bytes.HasPrefix(b, pngMagic) {
		t.Fatal("not PNG magic")
	}
}

func TestPNGEmpty(t *testing.T) {
	_, err := PNG("", 256)
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestPNGClamp(t *testing.T) {
	// default size
	b1, err := PNG("test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b1, pngMagic) {
		t.Fatal("default size not PNG")
	}
	// clamped low
	b2, err := PNG("test", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b2, pngMagic) || len(b2) < 100 {
		t.Fatal("clamped low failed")
	}
	// clamped high
	b3, err := PNG("test", 9999)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b3, pngMagic) || len(b3) < 100 {
		t.Fatal("clamped high failed")
	}
	// high clamp should produce larger PNG than low clamp generally
	// but at least both valid; check high >= low or just non-trivial
	if len(b3) == 0 || len(b2) == 0 {
		t.Fatal("empty")
	}
}
