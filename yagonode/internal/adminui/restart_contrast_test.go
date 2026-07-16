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
	stylesheet, err := assetFS.ReadFile("assets/photon.css")
	if err != nil {
		t.Fatalf("read stylesheet: %v", err)
	}
	for _, contract := range []string{
		"--ph-titlebar-hi: #7ba2e8",
		"--cds-header-bg: #5c8bdf",
		"background: linear-gradient(180deg, var(--ph-titlebar-hi) 0%, var(--cds-header-bg) 100%);",
		"color: var(--cds-text-primary);",
	} {
		if !strings.Contains(string(stylesheet), contract) {
			t.Fatalf("restart stylesheet missing %s", contract)
		}
	}

	dark := [3]float64{16, 16, 16}
	backgrounds := [][3]float64{{123, 162, 232}, {92, 139, 223}}
	for _, background := range backgrounds {
		if ratio := restartContrastRatio(dark, background); ratio < 4.5 {
			t.Fatalf("titlebar contrast = %.2f, want at least 4.5", ratio)
		}
	}
}
