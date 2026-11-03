package appearance

import "testing"

// TestConvertThemeTUIColorFallbacks covers the two §8.5 fallbacks that only
// fire when the authored value is absent: a readable SelectedFg for the
// selection background, and Thinking falling back to Dim.
func TestConvertThemeTUIColorFallbacks(t *testing.T) {
	raw := &RawTheme{
		Type: "dark",
		Colors: map[string]string{
			"editor.background":              "#000000",
			"editor.foreground":              "#eeeeee",
			"list.activeSelectionBackground": "#000000",
			"editorLineNumber.foreground":    "#777777",
		},
	}
	got, err := ConvertTheme("vscode:test/fallbacks", "Fallbacks", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.Colors.SelectedFg, "#eeeeee")
	assertEq(t, got.Colors.Dim, "#777777")
	assertEq(t, got.Colors.Thinking, "#777777")
}
