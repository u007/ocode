package tui

import (
	"strings"
	"testing"
)

func TestButtonRender(t *testing.T) {
	sb := NewScrollbar()
	_ = sb // suppress unused

	tests := []struct {
		name      string
		label     string
		variant   ButtonVariant
		hovered   bool
		focused   bool
		wantEmpty bool
		wantLabel bool // rendered output should contain the label
	}{
		{
			name:      "normal button renders label",
			label:     "OK",
			variant:   ButtonNormal,
			wantLabel: true,
		},
		{
			name:      "primary button renders label",
			label:     "Yes",
			variant:   ButtonPrimary,
			wantLabel: true,
		},
		{
			name:      "danger button renders label",
			label:     "Delete",
			variant:   ButtonDanger,
			wantLabel: true,
		},
		{
			name:      "hovered state renders label",
			label:     "Cancel",
			variant:   ButtonNormal,
			hovered:   true,
			wantLabel: true,
		},
		{
			name:      "focused state renders label",
			label:     "Yes",
			variant:   ButtonPrimary,
			focused:   true,
			wantLabel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			btn := NewButton(tt.label, tt.variant)
			btn.Hovered = tt.hovered
			btn.Focused = tt.focused
			result := btn.Render()

			if tt.wantEmpty && result != "" {
				t.Errorf("expected empty, got %q", result)
				return
			}
			if result == "" {
				t.Error("expected non-empty render")
				return
			}
			if tt.wantLabel && !strings.Contains(result, tt.label) {
				t.Errorf("rendered output %q does not contain label %q", result, tt.label)
			}
		})
	}
}

func TestButtonHoverDiffersFromIdle(t *testing.T) {
	btn := NewButton("OK", ButtonNormal)

	btn.Hovered = false
	idle := btn.Render()

	btn.Hovered = true
	hovered := btn.Render()

	if idle == hovered {
		t.Error("hovered state should render differently from idle")
	}
}

func TestButtonFocusedDiffersFromIdle(t *testing.T) {
	btn := NewButton("OK", ButtonPrimary)

	btn.Focused = false
	idle := btn.Render()

	btn.Focused = true
	focused := btn.Render()

	if idle == focused {
		t.Error("focused state should render differently from idle")
	}
}

func TestButtonContains(t *testing.T) {
	tests := []struct {
		name string
		x, y int
		want bool
	}{
		{
			name: "inside bounds",
			x:    2, y: 0,
			want: true,
		},
		{
			name: "outside bounds left",
			x:    -1, y: 0,
			want: false,
		},
		{
			name: "outside bounds right",
			x:    100, y: 0,
			want: false,
		},
		{
			name: "outside bounds top",
			x:    2, y: -1,
			want: false,
		},
		{
			name: "outside bounds bottom",
			x:    2, y: 10,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			btn := NewButton("OK", ButtonNormal)
			btn.X = 0
			btn.Y = 0
			btn.Width = 6 // len(" OK  ") padded
			btn.Height = 1
			got := btn.Contains(tt.x, tt.y)
			if got != tt.want {
				t.Errorf("Contains(%d, %d) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestButtonVariantsDiffer(t *testing.T) {
	normal := NewButton("OK", ButtonNormal).Render()
	primary := NewButton("OK", ButtonPrimary).Render()
	danger := NewButton("OK", ButtonDanger).Render()

	// At least primary and danger should differ from normal
	if normal == primary {
		t.Error("primary should render differently from normal")
	}
	if normal == danger {
		t.Error("danger should render differently from normal")
	}
}
