package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Pick chooses an option by its text, ported from htrcli's pick
// (internal/cdp/pick.go). A native <select> takes the option by value or
// visible text. Any other element is treated as a custom combobox (react-select,
// async autocomplete): the input is focused, ArrowDown opens the menu, the text
// is typed with Input.insertText, the options are polled, the best match is
// clicked, and the widget's displayed value is checked against it.

const (
	// pickOptionsWait bounds how long pick polls for a matching option after
	// typing (async autocompletes answer after a network round trip).
	pickOptionsWait = 3 * time.Second
	// pickVerifyWait bounds how long pick waits for the widget to display the
	// clicked option (react-select re-renders after the click).
	pickVerifyWait = 2 * time.Second
	pickPollEvery  = 100 * time.Millisecond
	// pickListMax caps the options listed in a failure message.
	pickListMax = 15
	// pickOptionTextMax clips each option's text in a failure message.
	pickOptionTextMax = 100
)

// PickResult reports what pick selected and how.
type PickResult struct {
	Mode     string `json:"mode"`     // "select" or "combobox"
	Selected string `json:"selected"` // the displayed selected value after the pick
}

// selectOption is one <option> of a native <select>.
type selectOption struct {
	Value string `json:"value"`
	Text  string `json:"text"`
}

// Pick chooses the option matching text in the element target names (an @eN
// ref or a selector). It returns the mode used and the displayed value after
// the pick. Every failure is prefixed "pick:" so the caller can see which verb
// failed.
func (t *Target) Pick(ctx context.Context, frame, target, text string, refs *RefTable) (PickResult, error) {
	if strings.TrimSpace(text) == "" {
		return PickResult{}, errors.New("pick: option text cannot be empty")
	}
	el, err := t.resolveInteract(ctx, frame, target, refs)
	if err != nil {
		return PickResult{}, fmt.Errorf("pick: %w", err)
	}
	if el.probe.Tag == "select" {
		shown, err := t.selectOption(ctx, el, text)
		if err != nil {
			return PickResult{}, fmt.Errorf("pick: %w", err)
		}
		return PickResult{Mode: "select", Selected: clipText(shown.Text, interactTextMax)}, nil
	}
	shown, err := t.pickCombobox(ctx, frame, el, text)
	if err != nil {
		return PickResult{}, err
	}
	return PickResult{Mode: "combobox", Selected: clipText(shown, interactTextMax)}, nil
}

// resolveSelectOption maps want onto an option: an exact value match first,
// else the best-ranked visible text (exact > prefix > contains).
func resolveSelectOption(options []selectOption, want string) (selectOption, error) {
	var byValue []selectOption
	for _, o := range options {
		if o.Value == want {
			byValue = append(byValue, o)
		}
	}
	if len(byValue) == 1 {
		return byValue[0], nil
	}
	texts := make([]string, len(options))
	for i, o := range options {
		texts[i] = o.Text
	}
	idx, tied := bestOption(texts, want)
	if tied != nil {
		return selectOption{}, fmt.Errorf("%q is ambiguous: it matches %s equally well", clipText(want, interactTextMax), listOptions(tied))
	}
	if idx < 0 {
		return selectOption{}, fmt.Errorf("no option matches %q; available: %s", clipText(want, interactTextMax), listOptions(texts))
	}
	return options[idx], nil
}

// normOption lower-cases, trims and collapses whitespace for comparison.
func normOption(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// optionRank scores how well an option text matches the wanted text:
// 3 exact, 2 prefix, 1 contains, 0 no match (all case-insensitive, after
// whitespace normalisation).
func optionRank(option, want string) int {
	o, w := normOption(option), normOption(want)
	switch {
	case w == "":
		return 0
	case o == w:
		return 3
	case strings.HasPrefix(o, w):
		return 2
	case strings.Contains(o, w):
		return 1
	default:
		return 0
	}
}

// bestOption picks the option that best matches want. It returns the index of
// the unique best match; -1 with a nil slice when nothing matches; or -1 with
// the tied option texts when several options share the best rank (ambiguous:
// the caller must not guess).
func bestOption(options []string, want string) (int, []string) {
	bestRank, best := 0, []int{}
	for i, o := range options {
		r := optionRank(o, want)
		switch {
		case r == 0:
		case r > bestRank:
			bestRank, best = r, []int{i}
		case r == bestRank:
			best = append(best, i)
		}
	}
	if len(best) == 0 {
		return -1, nil
	}
	if len(best) > 1 {
		tied := make([]string, len(best))
		for i, idx := range best {
			tied[i] = options[idx]
		}
		return -1, tied
	}
	return best[0], nil
}

// sameOptions reports whether two polled option lists are identical.
func sameOptions(a, b []string) bool {
	if a == nil || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listOptions renders up to pickListMax options for an error message, each one
// clipped so a page with long option labels cannot inflate the message.
func listOptions(options []string) string {
	if len(options) == 0 {
		return "(no options visible)"
	}
	shown := options
	more := ""
	if len(shown) > pickListMax {
		shown = shown[:pickListMax]
		more = fmt.Sprintf(" … and %d more", len(options)-pickListMax)
	}
	quoted := make([]string, len(shown))
	for i, o := range shown {
		quoted[i] = fmt.Sprintf("%q", clipText(o, pickOptionTextMax))
	}
	return strings.Join(quoted, ", ") + more
}

// pickSettled decides whether one poll of the option list is conclusive. An
// exact match is taken at once. A weaker (prefix/contains) match, or a tie
// between several options, is only final once the list is unchanged across two
// polls, so a stale or half-loaded async list cannot win over (or hide) the
// exact option still loading. A tie on a settled list is reported at once.
func pickSettled(prev, options []string, want string) (choice int, tied []string, settled bool) {
	choice, tied = bestOption(options, want)
	switch {
	case choice >= 0:
		settled = optionRank(options[choice], want) == 3 || sameOptions(prev, options)
	case tied != nil:
		settled = sameOptions(prev, options)
	}
	return choice, tied, settled
}

// waitStep sleeps for d, or returns early with the context's error.
func waitStep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// pickCombobox drives a custom combobox with trusted input. From the moment the
// query is typed, the menu is open, so every later failure presses Escape.
func (t *Target) pickCombobox(ctx context.Context, frame string, el interactTarget, want string) (string, error) {
	raw, err := t.callKitIn(ctx, el.contextID, "comboPrepare", el.path)
	if err != nil {
		return "", err
	}
	var prep struct {
		Focused bool `json:"focused"`
	}
	if err := json.Unmarshal(raw, &prep); err != nil {
		return "", fmt.Errorf("decoding combobox prepare: %w", err)
	}
	if !prep.Focused {
		return "", fmt.Errorf("pick: could not focus the combobox input at %s", clipText(el.raw, interactTextMax))
	}
	if err := t.PressKey(ctx, "ArrowDown"); err != nil {
		return "", fmt.Errorf("pick: opening the menu: %w", err)
	}
	if err := t.InsertText(ctx, want); err != nil {
		return "", fmt.Errorf("pick: typing %q: %w", clipText(want, interactTextMax), err)
	}
	fail := func(err error) (string, error) {
		if perr := t.PressKey(ctx, "Escape"); perr != nil {
			err = fmt.Errorf("%w (closing the menu with Escape also failed: %v)", err, perr)
		}
		return "", err
	}

	var options, prev, tied []string
	choice := -1
	for deadline := time.Now().Add(pickOptionsWait); ; {
		prev = options
		options, err = t.comboOptions(ctx, el)
		if err != nil {
			return fail(err)
		}
		var settled bool
		choice, tied, settled = pickSettled(prev, options, want)
		if settled || time.Now().After(deadline) {
			break
		}
		if err := waitStep(ctx, pickPollEvery); err != nil {
			return fail(err)
		}
	}
	if tied != nil {
		return fail(fmt.Errorf("pick: %q is ambiguous: it matches %s equally well; pass the full option text", clipText(want, interactTextMax), listOptions(tied)))
	}
	if choice < 0 {
		if len(options) == 0 {
			// The typed text filtered everything out: show the unfiltered menu
			// so the caller sees what the widget actually offers.
			available, err := t.comboUnfilteredOptions(ctx, el)
			if err != nil {
				return fail(fmt.Errorf("pick: no option matching %q appeared within %s (listing the unfiltered menu also failed: %w)", clipText(want, interactTextMax), pickOptionsWait, err))
			}
			return "", fmt.Errorf("pick: no option matching %q appeared within %s; the menu without a query offers: %s", clipText(want, interactTextMax), pickOptionsWait, listOptions(available))
		}
		return fail(fmt.Errorf("pick: no option matching %q appeared within %s; available: %s", clipText(want, interactTextMax), pickOptionsWait, listOptions(options)))
	}
	chosen := options[choice]

	lx, ly, err := t.optionPoint(ctx, el, chosen)
	if err != nil {
		return fail(fmt.Errorf("pick: locating option %q: %w", clipText(chosen, pickOptionTextMax), err))
	}
	x, y, err := t.frameToTop(ctx, frame, chosen, lx, ly)
	if err != nil {
		return fail(err)
	}
	if err := t.Click(ctx, x, y); err != nil {
		return fail(fmt.Errorf("pick: clicking option %q: %w", clipText(chosen, pickOptionTextMax), err))
	}

	// Verify against the option actually clicked, not the (fuzzy) request: an
	// empty or partial request would match any stale label or chip.
	var shown struct {
		Text string `json:"text"`
		From string `json:"from"`
	}
	for deadline := time.Now().Add(pickVerifyWait); ; {
		raw, err := t.callKitIn(ctx, el.contextID, "comboValue", el.path)
		if err != nil {
			return fail(err)
		}
		if err := json.Unmarshal(raw, &shown); err != nil {
			return fail(fmt.Errorf("decoding combobox value: %w", err))
		}
		if c := normOption(chosen); c != "" && strings.Contains(normOption(shown.Text), c) {
			return shown.Text, nil
		}
		if time.Now().After(deadline) {
			break
		}
		if err := waitStep(ctx, pickPollEvery); err != nil {
			return fail(err)
		}
	}
	return fail(fmt.Errorf("pick: clicked option %q at (%.0f, %.0f) but after %s the combobox shows %q (read from %s), which does not contain %q; options were: %s",
		clipText(chosen, pickOptionTextMax), x, y, pickVerifyWait,
		clipText(shown.Text, interactTextMax), clipText(shown.From, interactTextMax),
		clipText(want, interactTextMax), listOptions(options)))
}

// comboOptions lists the visible options of the combobox el.
func (t *Target) comboOptions(ctx context.Context, el interactTarget) ([]string, error) {
	raw, err := t.callKitIn(ctx, el.contextID, "comboOptions", el.path)
	if err != nil {
		return nil, err
	}
	var options []string
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, fmt.Errorf("decoding combobox options: %w", err)
	}
	return options, nil
}

// comboUnfilteredOptions clears the query, reopens the menu with ArrowDown,
// reads the options it shows, then closes it with Escape. It is used only to
// enrich a no-match error.
func (t *Target) comboUnfilteredOptions(ctx context.Context, el interactTarget) ([]string, error) {
	if _, err := t.callKitIn(ctx, el.contextID, "comboPrepare", el.path); err != nil {
		return nil, err
	}
	if err := t.PressKey(ctx, "ArrowDown"); err != nil {
		return nil, err
	}
	var options []string
	for deadline := time.Now().Add(pickVerifyWait); ; {
		var err error
		options, err = t.comboOptions(ctx, el)
		if err != nil {
			return nil, err
		}
		if len(options) > 0 || time.Now().After(deadline) {
			break
		}
		if err := waitStep(ctx, pickPollEvery); err != nil {
			return nil, err
		}
	}
	if err := t.PressKey(ctx, "Escape"); err != nil {
		return nil, err
	}
	return options, nil
}

// optionPoint returns the frame-local viewport centre of the one visible option
// whose text is option. The form kit scrolls it into view first.
func (t *Target) optionPoint(ctx context.Context, el interactTarget, option string) (float64, float64, error) {
	raw, err := t.callKitIn(ctx, el.contextID, "optionPoint", el.path, option)
	if err != nil {
		return 0, 0, err
	}
	var pt struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.Unmarshal(raw, &pt); err != nil {
		return 0, 0, fmt.Errorf("decoding option point: %w", err)
	}
	return pt.X, pt.Y, nil
}

// frameToTop converts an option point from the frame's viewport to the top
// viewport. Outside a frame the point is already top-viewport. A point the click
// would miss is an error. The frame origin comes from frameOrigin, the same
// helper ClickElement uses, so pick and click map an iframe identically
// (including a CSS transform and sub-pixel placement on the <iframe>).
//
// Keep pick and click on one frame-origin helper.
func (t *Target) frameToTop(ctx context.Context, frame, option string, x, y float64) (float64, float64, error) {
	if frame == "" {
		return x, y, nil
	}
	ox, oy, err := t.frameOrigin(ctx, frame)
	if err != nil {
		return 0, 0, err
	}
	tx, ty := ox+x, oy+y
	vp, err := t.viewportRect(ctx)
	if err != nil {
		return 0, 0, err
	}
	if tx < 0 || ty < 0 || tx >= vp.x1 || ty >= vp.y1 {
		return 0, 0, fmt.Errorf("pick: option %q is at (%.0f, %.0f), outside the top viewport (%.0fx%.0f); a click there would miss: scroll the frame into view first",
			clipText(option, pickOptionTextMax), tx, ty, vp.x1, vp.y1)
	}
	return tx, ty, nil
}
