package qr

import (
	"errors"

	qrcode "github.com/skip2/go-qrcode"
)

// PNG renders payload as PNG QR code of size pixels square.
// Clamped to 128..1024, default 512 if <=0. Medium error correction.
func PNG(payload string, size int) ([]byte, error) {
	if payload == "" {
		return nil, errors.New("empty payload")
	}
	if size <= 0 {
		size = 512
	} else if size < 128 {
		size = 128
	} else if size > 1024 {
		size = 1024
	}
	return qrcode.Encode(payload, qrcode.Medium, size)
}
