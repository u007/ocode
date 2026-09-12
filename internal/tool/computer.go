package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"sync"
	"time"

	"github.com/u007/ocode/internal/config"
	"golang.org/x/image/draw"
)

// ComputerMaxImageSide is the longest side (in pixels) a screenshot
// is downscaled to before being returned to the agent.
const ComputerMaxImageSide = 1568

// ComputerTool drives the host desktop: screenshots, mouse, and
// keyboard. It is opt-in via cfg.Ocode.ComputerUse.Enabled and is
// not advertised by InitBuiltinTools when disabled.
type ComputerTool struct {
	Config  *config.Config
	Driver  ComputerDriver
	mu      sync.Mutex // guards scaleState below
	screenW int  // OS input space width from last Screenshot
	screenH int  // OS input space height from last Screenshot
	imgW    int  // scaled PNG width
	imgH    int  // scaled PNG height
	lastPNG []byte // scaled PNG bytes (nil until first screenshot)
}

// scaleState holds the ratio between OS input coordinates and
// the scaled PNG pixel coordinates.
type scaleState struct {
	scaleX float64
	scaleY float64
}

// Definition returns the Anthropic-style computer-use tool definition.
func (t *ComputerTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "computer",
		"description": "Control the host desktop: screenshot, click, move, drag, scroll, type, and press keys. Coordinates are in the pixel space of the most recent screenshot. Take a screenshot first to know what you're doing. Each action returns one line.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{
						"screenshot", "left_click", "right_click", "middle_click",
						"double_click", "mouse_move", "left_click_drag",
						"scroll", "type", "key", "cursor_position", "wait",
					},
					"description": "The action to perform.",
				},
				"coordinate": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{"type": "integer"},
					"description": "Target [x, y] in the pixel space of the most recent screenshot.",
				},
				"start_coordinate": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{"type": "integer"},
					"description": "Start [x, y] for drag actions, in screenshot pixel space.",
				},
				"text": map[string]interface{}{
					"type": "string",
					"description": "Text to type (action=type) or key combo (action=key).",
				},
				"scroll_direction": map[string]interface{}{
					"type": "string",
					"enum": []string{"up", "down", "left", "right"},
					"description": "Direction to scroll (action=scroll).",
				},
				"scroll_amount": map[string]interface{}{
					"type": "integer",
					"default": 3,
					"description": "Number of scroll ticks (action=scroll).",
				},
				"duration": map[string]interface{}{
					"type": "number",
					"maximum": 10,
					"description": "Seconds to wait (action=wait).",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *ComputerTool) Name() string { return "computer" }

func (t *ComputerTool) Description() string {
	return "Control the host desktop: screenshot, click, move, drag, scroll, type, and press keys."
}

func (t *ComputerTool) Parallel() bool { return false }

// Execute delegates to ExecuteCtx with a background context.
func (t *ComputerTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

// ExecuteCtx validates the action, scales coordinates, calls the
// driver, and returns a one-line result.
func (t *ComputerTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Action          string   `json:"action"`
		Coordinate      []int    `json:"coordinate"`
		StartCoordinate []int    `json:"start_coordinate"`
		Text            string   `json:"text"`
		ScrollDirection string   `json:"scroll_direction"`
		ScrollAmount    int      `json:"scroll_amount"`
		Duration        float64  `json:"duration"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("computer: invalid arguments: %w", err)
	}

	if params.ScrollAmount == 0 {
		params.ScrollAmount = 3
	}

	if err := validateAction(params); err != nil {
		return "", err
	}

	t.mu.Lock()
	hasScreenshot := t.lastPNG != nil
	sw, sh := t.screenW, t.screenH
	iw, ih := t.imgW, t.imgH
	t.mu.Unlock()

	if !hasScreenshot && params.Action != "screenshot" && params.Action != "wait" {
		// Seed scale state with a throwaway screenshot.
		rawPNG, w, h, err := t.Driver.Screenshot(ctx)
		if err != nil {
			return "", fmt.Errorf("computer: screenshot seed failed: %w", err)
		}
		scaled, iw2, ih2, err := scalePNG(rawPNG, ComputerMaxImageSide)
		if err != nil {
			return "", fmt.Errorf("computer: scale seed failed: %w", err)
		}
		t.mu.Lock()
		t.screenW, t.screenH = w, h
		t.imgW, t.imgH = iw2, ih2
		t.lastPNG = scaled
		t.mu.Unlock()
		sw, sh, iw, ih = w, h, iw2, ih2
	}

	var state scaleState
	if iw > 0 {
		state = scaleState{scaleX: float64(sw) / float64(iw), scaleY: float64(sh) / float64(ih)}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if params.Action == "type" {
		cancel()
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	}
	defer cancel()

	switch params.Action {
	case "screenshot":
		rawPNG, w, h, err := t.Driver.Screenshot(ctx)
		if err != nil {
			return "", fmt.Errorf("computer: screenshot: %w", err)
		}
		scaled, iw2, ih2, err := scalePNG(rawPNG, ComputerMaxImageSide)
		if err != nil {
			return "", fmt.Errorf("computer: scale: %w", err)
		}
		t.mu.Lock()
		t.screenW, t.screenH = w, h
		t.imgW, t.imgH = iw2, ih2
		t.lastPNG = scaled
		t.mu.Unlock()
		scale := float64(sw) / float64(iw2)
		return fmt.Sprintf("screen %dx%d, image %dx%d, scale %.2f", sw, sh, iw2, ih2, scale), nil

	case "left_click":
		x, y := toDriverCoords(params.Coordinate, state)
		if err := validateCoord(x, y, iw, ih); err != nil {
			return "", err
		}
		if err := t.Driver.Click(ctx, x, y, MouseLeft, 1); err != nil {
			return "", fmt.Errorf("computer: left_click: %w", err)
		}
		return fmt.Sprintf("left_click at %d,%d", params.Coordinate[0], params.Coordinate[1]), nil

	case "right_click":
		x, y := toDriverCoords(params.Coordinate, state)
		if err := validateCoord(x, y, iw, ih); err != nil {
			return "", err
		}
		if err := t.Driver.Click(ctx, x, y, MouseRight, 1); err != nil {
			return "", fmt.Errorf("computer: right_click: %w", err)
		}
		return fmt.Sprintf("right_click at %d,%d", params.Coordinate[0], params.Coordinate[1]), nil

	case "middle_click":
		x, y := toDriverCoords(params.Coordinate, state)
		if err := validateCoord(x, y, iw, ih); err != nil {
			return "", err
		}
		if err := t.Driver.Click(ctx, x, y, MouseMiddle, 1); err != nil {
			return "", fmt.Errorf("computer: middle_click: %w", err)
		}
		return fmt.Sprintf("middle_click at %d,%d", params.Coordinate[0], params.Coordinate[1]), nil

	case "double_click":
		x, y := toDriverCoords(params.Coordinate, state)
		if err := validateCoord(x, y, iw, ih); err != nil {
			return "", err
		}
		if err := t.Driver.Click(ctx, x, y, MouseLeft, 2); err != nil {
			return "", fmt.Errorf("computer: double_click: %w", err)
		}
		return fmt.Sprintf("double_click at %d,%d", params.Coordinate[0], params.Coordinate[1]), nil

	case "mouse_move":
		x, y := toDriverCoords(params.Coordinate, state)
		if err := validateCoord(x, y, iw, ih); err != nil {
			return "", err
		}
		if err := t.Driver.Move(ctx, x, y); err != nil {
			return "", fmt.Errorf("computer: mouse_move: %w", err)
		}
		return fmt.Sprintf("moved to %d,%d", params.Coordinate[0], params.Coordinate[1]), nil

	case "left_click_drag":
		x1, y1 := toDriverCoords(params.StartCoordinate, state)
		x2, y2 := toDriverCoords(params.Coordinate, state)
		if err := validateCoord(x1, y1, iw, ih); err != nil {
			return "", err
		}
		if err := validateCoord(x2, y2, iw, ih); err != nil {
			return "", err
		}
		if err := t.Driver.Drag(ctx, x1, y1, x2, y2); err != nil {
			return "", fmt.Errorf("computer: left_click_drag: %w", err)
		}
		return fmt.Sprintf("dragged %d,%d -> %d,%d", params.StartCoordinate[0], params.StartCoordinate[1], params.Coordinate[0], params.Coordinate[1]), nil

	case "scroll":
		x, y := toDriverCoords(params.Coordinate, state)
		if err := validateCoord(x, y, iw, ih); err != nil {
			return "", err
		}
		if err := t.Driver.Scroll(ctx, x, y, params.ScrollDirection, params.ScrollAmount); err != nil {
			return "", fmt.Errorf("computer: scroll: %w", err)
		}
		return fmt.Sprintf("scrolled %s %d at %d,%d", params.ScrollDirection, params.ScrollAmount, params.Coordinate[0], params.Coordinate[1]), nil

	case "type":
		if err := t.Driver.Type(ctx, params.Text); err != nil {
			return "", fmt.Errorf("computer: type: %w", err)
		}
		return fmt.Sprintf("typed %d chars", len(params.Text)), nil

	case "key":
		if err := t.Driver.Key(ctx, params.Text); err != nil {
			return "", fmt.Errorf("computer: key: %w", err)
		}
		return fmt.Sprintf("pressed %s", params.Text), nil

	case "cursor_position":
		x, y, err := t.Driver.Cursor(ctx)
		if err != nil {
			return "", fmt.Errorf("computer: cursor_position: %w", err)
		}
		sx := int(math.Round(float64(x) / state.scaleX))
		sy := int(math.Round(float64(y) / state.scaleY))
		return fmt.Sprintf("cursor %d,%d", sx, sy), nil

	case "wait":
		select {
		case <-time.After(time.Duration(params.Duration * float64(time.Second))):
			return fmt.Sprintf("waited %.0fs", params.Duration), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}

	default:
		return "", fmt.Errorf("computer: unknown action %q", params.Action)
	}
}

// ExecuteImage returns the last screenshot PNG or an error if no
// screenshot has been taken.
func (t *ComputerTool) ExecuteImage(args json.RawMessage) (raw []byte, mimeType string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastPNG == nil {
		return nil, "", fmt.Errorf("computer: no screenshot has been taken yet")
	}
	return t.lastPNG, "image/png", nil
}

// ProducesImage returns true only when the action is "screenshot".
func (t *ComputerTool) ProducesImage(args json.RawMessage) bool {
	var params struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return false
	}
	return params.Action == "screenshot"
}

func validateAction(p struct {
	Action          string   `json:"action"`
	Coordinate      []int    `json:"coordinate"`
	StartCoordinate []int    `json:"start_coordinate"`
	Text            string   `json:"text"`
	ScrollDirection string   `json:"scroll_direction"`
	ScrollAmount    int      `json:"scroll_amount"`
	Duration        float64  `json:"duration"`
}) error {
	switch p.Action {
	case "screenshot", "wait":
		// No required coordinates/text
	case "left_click", "right_click", "middle_click", "double_click", "mouse_move", "scroll":
		if len(p.Coordinate) != 2 || p.Coordinate[0] < 0 || p.Coordinate[1] < 0 {
			return fmt.Errorf("computer: %s requires a valid coordinate [x,y]", p.Action)
		}
	case "left_click_drag":
		if len(p.StartCoordinate) != 2 || p.StartCoordinate[0] < 0 || p.StartCoordinate[1] < 0 {
			return fmt.Errorf("computer: left_click_drag requires start_coordinate [x,y]")
		}
		if len(p.Coordinate) != 2 || p.Coordinate[0] < 0 || p.Coordinate[1] < 0 {
			return fmt.Errorf("computer: left_click_drag requires coordinate [x,y]")
		}
	case "type", "key":
		if p.Text == "" {
			return fmt.Errorf("computer: %s requires text", p.Action)
		}
	default:
		return fmt.Errorf("computer: unknown action %q", p.Action)
	}

	if p.ScrollDirection != "" {
		switch p.ScrollDirection {
		case "up", "down", "left", "right":
		default:
			return fmt.Errorf("computer: unknown scroll_direction %q", p.ScrollDirection)
		}
	}

	if p.Duration < 0 || p.Duration > 10 {
		return fmt.Errorf("computer: duration must be between 0 and 10 seconds")
	}

	return nil
}

func toDriverCoords(coord []int, s scaleState) (int, int) {
	if s.scaleX == 0 || s.scaleY == 0 {
		return coord[0], coord[1]
	}
	return int(math.Round(float64(coord[0]) * s.scaleX)), int(math.Round(float64(coord[1]) * s.scaleY))
}

func validateCoord(x, y, imgW, imgH int) error {
	if x < 0 || y < 0 || (imgW > 0 && x >= imgW) || (imgH > 0 && y >= imgH) {
		return fmt.Errorf("computer: coordinates (%d,%d) out of bounds (image %dx%d)", x, y, imgW, imgH)
	}
	return nil
}

// scalePNG downscales a PNG so its longest side is at most maxSide.
// If the image is already within bounds, the original bytes are
// returned unchanged.
func scalePNG(raw []byte, maxSide int) (out []byte, w, h int, err error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, err
	}
	bounds := img.Bounds()
	w = bounds.Max.X - bounds.Min.X
	h = bounds.Max.Y - bounds.Min.Y
	if w <= maxSide && h <= maxSide {
		return raw, w, h, nil
	}
	var scale float64
	if w >= h {
		scale = float64(maxSide) / float64(w)
	} else {
		scale = float64(maxSide) / float64(h)
	}
	newW := int(math.Round(float64(w) * scale))
	newH := int(math.Round(float64(h) * scale))
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), newW, newH, nil
}
