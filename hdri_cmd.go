package main

import (
	"fmt"
	"strings"
)

func runHDRICmd(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "", "doctor", "status", "info", "help", "-h", "--help":
		fmt.Print(FormatHDRIDoctor())
		fmt.Println()
		fmt.Println("usage:")
		fmt.Println("  gy hdri doctor")
		fmt.Println("  GrokGlyph: cam → scene order → HDRI (export + cast to sphere)")
		fmt.Println("  slots: laptop C · phone ?slot=L1 / R1 · multi-cam simulcast")
		fmt.Println()
		fmt.Println("pipeline:")
		fmt.Println("  1. Laptop: grokglyph.html → cam (webcam = C)")
		fmt.Println("  2. Phones: ?slot=L1 and ?slot=R1 → cam → cast")
		fmt.Println("  3. HDRI: freeze lanes → subject strip + equirect probe → download / sphere")
		return nil
	default:
		fmt.Print(FormatHDRIDoctor())
		return nil
	}
}
