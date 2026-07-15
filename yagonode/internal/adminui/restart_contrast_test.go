package adminui

import (
	"math"
	"strings"
	"testing"
)

func restartRelativeLuminance(rgb [3]float64) float64 {
	for channel, value := range rgb {
		value /= 255
		if value <= 0.04045 {
			rgb[channel] = value / 12.92
		} else {
			rgb[channel] = math.Pow((value+0.055)/1.055, 2.4)
		}
	}

	return 0.2126*rgb[0] + 0.7152*rgb[1] + 0.0722*rgb[2]
}

func restartContrastRatio(foreground, background [3]float64) float64 {
	first := restartRelativeLuminance(foreground)
	second := restartRelativeLuminance(background)
	if first < second {
		first, second = second, first
	}

	return (first + 0.05) / (second + 0.05)
}

func TestRestartBannerTextMeetsWCAGAAContrast(t *testing.T) {
	stylesheet, err := assetFS.ReadFile("assets/carbon.css")
	if err != nil {
		t.Fatalf("read stylesheet: %v", err)
	}
	for _, color := range []string{"#304f88", "#24447d", "#18366f", "#ffb3b8"} {
		if !strings.Contains(string(stylesheet), color) {
			t.Fatalf("restart stylesheet missing %s", color)
		}
	}

	white := [3]float64{255, 255, 255}
	brand := [3]float64{255, 179, 184}
	backgrounds := [][3]float64{{48, 79, 136}, {36, 68, 125}, {24, 54, 111}}
	for _, background := range backgrounds {
		if ratio := restartContrastRatio(white, background); ratio < 4.5 {
			t.Fatalf("white contrast = %.2f, want at least 4.5", ratio)
		}
		if ratio := restartContrastRatio(brand, background); ratio < 4.5 {
			t.Fatalf("brand contrast = %.2f, want at least 4.5", ratio)
		}
	}
}
