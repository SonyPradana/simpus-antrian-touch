package main

import "github.com/egoist/mygo/ui"

// Kanagawa Lotus palette. Only the darkest variants are used for text and
// buttons so contrast stays ≥ 7:1 on a kiosk screen that often catches light.
var (
	lotusWhite3 = ui.Hex("#f2ecbc") // main background
	lotusWhite2 = ui.Hex("#e5ddb0") // secondary card background
	lotusGray2  = ui.Hex("#716e61") // lines and borders
	lotusInk2   = ui.Hex("#43436c") // main text, 7.7:1 on lotusWhite3
	lotusInk1   = ui.Hex("#545464") // secondary text
	lotusBlue5  = ui.Hex("#5d57a3") // primary button accent
	lotusBlue4  = ui.Hex("#4d699b") // accent while pressed
	lotusGreen  = ui.Hex("#6f894e")
	lotusRed    = ui.Hex("#c84053")
	lotusOrange = ui.Hex("#cc6d00")
)

// LotusTheme returns the kiosk theme: light background, dark text, large
// touch targets via raised Spacing and FontSize.
func LotusTheme() *ui.Theme {
	t := ui.LightTheme()
	t.Background = lotusWhite3
	t.Surface = lotusWhite2
	t.SurfaceHover = ui.Hex("#ded5a6")
	t.SurfacePressed = ui.Hex("#d5cea3")
	t.Border = lotusGray2
	t.Text = lotusInk2
	t.TextMuted = lotusInk1
	t.Accent = lotusBlue5
	t.AccentHover = lotusBlue4
	t.AccentPressed = ui.Hex("#4a4590")
	t.AccentText = ui.Hex("#ffffff")
	t.Danger = lotusRed
	t.Warning = lotusOrange
	t.Success = lotusGreen
	t.Selection = ui.RGBA(93, 87, 163, 0.25)
	t.Focus = ui.RGBA(67, 67, 108, 0.75)
	t.FocusRingWidth = 4
	t.FocusRingOffset = 2
	t.InactiveAccent = lotusBlue5
	t.Radius = 14
	t.Spacing = 8
	t.FontSize = 20
	t.ScrollbarWidth = 14
	t.Scrollbar = ui.RGBA(67, 67, 108, 0.35)
	return t
}
