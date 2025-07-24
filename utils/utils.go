package utils

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/logrusorgru/aurora"
)

func GetRandomColorForLog() aurora.Color {
	colors := []aurora.Color{
		aurora.RedFg,
		aurora.GreenFg,
		aurora.YellowFg,
		aurora.CyanFg,
		aurora.BlueFg,
		aurora.MagentaFg,
	}
	return colors[rand.Intn(len(colors))]
}

// hexToRGB, #RRGGBB formatındaki bir string'i R, G, B int değerlerine dönüştürür.
func hexToRGB(hex string) (r, g, b int, err error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("geçersiz HEX formatı: %s", hex)
	}

	rVal, err := strconv.ParseInt(hex[0:2], 16, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	gVal, err := strconv.ParseInt(hex[2:4], 16, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	bVal, err := strconv.ParseInt(hex[4:6], 16, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(rVal), int(gVal), int(bVal), nil
}


func rgbToAnsi256(r, g, b int) aurora.Color {
	if r == 0 && g == 0 && b == 0 {
		return aurora.BlackFg
	}
	if r == 255 && g == 0 && b == 0 {
		return aurora.RedFg
	}
	if r == 0 && g == 255 && b == 0 {
		return aurora.GreenFg
	}
	if r == 255 && g == 255 && b == 0 {
		return aurora.YellowFg
	}
	if r == 0 && g == 0 && b == 255 {
		return aurora.BlueFg
	}
	if r == 255 && g == 0 && b == 255 {
		return aurora.MagentaFg
	}
	if r == 0 && g == 255 && b == 255 {
		return aurora.CyanFg
	}
	if r == 255 && g == 255 && b == 255 {
		return aurora.WhiteFg
	}


	rNorm := int(float64(r) * 5 / 255.0)
	gNorm := int(float64(g) * 5 / 255.0)
	bNorm := int(float64(b) * 5 / 255.0)

	if rNorm >= 0 && rNorm <= 5 && gNorm >= 0 && gNorm <= 5 && bNorm >= 0 && bNorm <= 5 {
		index := 16 + 36*rNorm + 6*gNorm + bNorm
		return aurora.Index(uint8(index), nil).Color()
	}


	if r == g && g == b { 
		if r >= 8 && r <= 248 { 
			index := 232 + int(float64(r-8)/240*23)
			return aurora.Index(uint8(index), nil).Color()
		}
	}


	return aurora.BlueFg
}


func GetColorFromHex(hexCode string) aurora.Color {
	r, g, b, err := hexToRGB(hexCode)
	if err != nil {
		fmt.Printf("HEX rengi dönüştürme hatası: %v. Varsayılan renk kullanılıyor.\n", err)
		return GetRandomColorForLog()  
	}
	return rgbToAnsi256(r, g, b)
}

func Timer(name string) func() {
	start := time.Now()
	return func() {
		// Düzeltme 1: fmt.Sprintf kullanarak string formatla
		message := fmt.Sprintf("⏱️ %s %v sürede tamamlandı", name, time.Since(start))

		// Düzeltme 2: aurora.White() sadece string alır, sonra fmt.Println ile yazdır
		fmt.Println(aurora.White(message))

		// Alternatif kullanım:
		// fmt.Println(aurora.White(fmt.Sprintf("⏱️ %s %v sürede tamamlandı", name, time.Since(start))))
	}
}
