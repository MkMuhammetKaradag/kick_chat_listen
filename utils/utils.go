package utils

import (
	"fmt"
	"math/rand"
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
