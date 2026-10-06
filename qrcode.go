package main

import (
	"fmt"
	"io"
	"strings"

	"rsc.io/qr"
)

// printQR draws text as a QR code using half-block characters, so each line
// of output holds two rows of the code. Colours are forced to black on white
// because phone scanners struggle with inverted codes on dark terminals.
func printQR(w io.Writer, text string) error {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return err
	}
	const quiet = 2 // blank border, in modules
	black := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	size := code.Size + 2*quiet
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		b.WriteString("\x1b[30;107m") // black on bright white
		for x := 0; x < size; x++ {
			top, bottom := black(x, y), black(x, y+1) && y+1 < size
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	_, err = fmt.Fprint(w, b.String())
	return err
}
