package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// FillForm applies many fields in one call, ported from htrcli's fill-form
// (internal/cdp/fillform.go). Fields are applied in order, a failing field does
// not stop the rest, and afterwards every field is read back and given a
// status: OK, MISMATCH (written but not what it reads back as), FAILED (could
// not be written) or SKIPPED (JSON null).

const (
	// fieldsMax caps the fields in one call. A larger form is refused, not truncated.
	fieldsMax = 50
)

// Field kinds, detected from the matched element.
const (
	kindText     = "text"
	kindSelect   = "select"
	kindCheckbox = "checkbox"
	kindRadio    = "radio"
	kindFile     = "file"
	kindCombobox = "combobox"
)

// Field statuses in a fill-form report.
const (
	FieldOK       = "OK"
	FieldMismatch = "MISMATCH"
	FieldFailed   = "FAILED"
	FieldSkipped  = "SKIPPED"
)

// FormField is one field of a fill-form call: a selector and the raw JSON value
// for whatever element it matches. A JSON null value skips the field.
type FormField struct {
	Selector string          `json:"selector"`
	Value    json.RawMessage `json:"value"`
}

// FieldReport is one row of the fill-form read-back table. Selector, Requested
// and Actual are clipped to interactTextMax, and Error to interactErrMax.
type FieldReport struct {
	Selector  string `json:"selector"`
	Kind      string `json:"kind,omitempty"`
	Requested string `json:"requested"`
	Actual    string `json:"actual"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// FillForm applies fields in order, then reads every field back. A field's
// selector is an @eN ref or a selector, resolved as the other interact actions
// resolve theirs: a ref comes from refs (snapshot or find mints it), so an
// unknown or stale ref is that field's FAILED row with the reason, and the other
// fields still run. The returned error is for the whole call (no fields, too
// many, no allowed callback, a frame that cannot be reached); a per-field
// problem is a FAILED or MISMATCH row.
//
// allowed is the caller's path policy. A file field's paths are checked by
// validateUploadPaths before any of them reaches the browser. A nil allowed is
// an error, never a silent allow. Relative file paths are refused at this layer
// (validateUploadPaths requires absolute paths); the caller converts them first.
func (t *Target) FillForm(ctx context.Context, frame string, fields []FormField, refs *RefTable, allowed func(path string) error) ([]FieldReport, error) {
	if allowed == nil {
		return nil, errors.New("fill-form: nil allowed callback; the caller must supply the upload path policy")
	}
	if len(fields) == 0 {
		return nil, errors.New("fill-form: no fields given")
	}
	if len(fields) > fieldsMax {
		return nil, fmt.Errorf("fill-form: %d fields exceeds the cap of %d; split the form into several calls", len(fields), fieldsMax)
	}
	if _, err := t.realmFor(ctx, frame); err != nil {
		return nil, fmt.Errorf("fill-form: %w", err)
	}

	reports := make([]FieldReport, len(fields))
	values := make([]formValue, len(fields))
	// confirmed[i] is the state the fill verifiably produced (the <select>
	// option text or the combobox label pick verified after writing).
	// Read-back compares against it instead of re-matching the raw request.
	confirmed := make([]string, len(fields))
	for i, f := range fields {
		r := &reports[i]
		r.Selector = clipText(f.Selector, interactTextMax)
		v, err := decodeFormValue(f.Value)
		if err != nil {
			r.Status, r.Error = FieldFailed, clipText(err.Error(), interactErrMax)
			continue
		}
		values[i] = v
		r.Requested = clipText(v.String(), interactTextMax)
		if v.null {
			r.Status = FieldSkipped
			continue
		}
		kind, resolved, ferr := t.applyField(ctx, frame, f.Selector, v, refs, allowed, &confirmed[i])
		r.Kind = kind
		if resolved != nil {
			// Read-back compares the names the browser holds with the files it was given.
			values[i] = formValue{list: resolved}
		}
		if ferr != nil {
			r.Status, r.Error = FieldFailed, clipText(ferr.Error(), interactErrMax)
		}
	}

	// Read back after ALL writes, so a later field clobbering an earlier one
	// (dependent selects, re-rendering widgets) is caught.
	for i, f := range fields {
		r := &reports[i]
		if r.Status == FieldSkipped || r.Kind == "" {
			continue
		}
		actual, ok, err := t.readBackField(ctx, frame, f.Selector, r.Kind, confirmed[i], values[i], refs)
		r.Actual = clipText(actual, interactTextMax)
		switch {
		case r.Status == FieldFailed:
			if err != nil {
				r.Error = clipText(r.Error+"; read-back: "+err.Error(), interactErrMax)
			}
		case err != nil:
			r.Status, r.Error = FieldFailed, clipText("read-back: "+err.Error(), interactErrMax)
		case ok:
			r.Status = FieldOK
		default:
			r.Status = FieldMismatch
		}
	}
	return reports, nil
}

// applyField writes one value and returns the detected kind. For <select> and
// combobox fields it records in *confirmed what the fill verifiably produced.
// On failure *confirmed is left untouched. For a file field it also returns the
// resolved paths that were sent.
func (t *Target) applyField(ctx context.Context, frame, selector string, v formValue, refs *RefTable, allowed func(string) error, confirmed *string) (string, []string, error) {
	el, err := t.resolveInteract(ctx, frame, selector, refs)
	if err != nil {
		return "", nil, err
	}
	kind, err := elementKind(el.probe)
	if err != nil {
		return "", nil, err
	}
	switch kind {
	case kindText:
		if !v.isStr {
			return kind, nil, fmt.Errorf("text field needs a string value, got %s", v)
		}
		return kind, nil, t.writeText(ctx, el, v.str)
	case kindSelect:
		if !v.isStr {
			return kind, nil, fmt.Errorf("<select> needs a string value (option value or visible text), got %s", v)
		}
		opt, err := t.selectOption(ctx, el, v.str)
		if err == nil {
			*confirmed = opt.Text
		}
		return kind, nil, err
	case kindCheckbox:
		if !v.isB {
			return kind, nil, fmt.Errorf("checkbox needs true or false, got %q", v.String())
		}
		return kind, nil, t.writeChecked(ctx, el, v.b)
	case kindRadio:
		if !v.isB {
			return kind, nil, fmt.Errorf("radio needs true, got %q", v.String())
		}
		if !v.b {
			return kind, nil, errors.New("a radio cannot be unchecked directly; set another radio in its group to true")
		}
		return kind, nil, t.writeChecked(ctx, el, true)
	case kindFile:
		paths, err := formPaths(v)
		if err != nil {
			return kind, nil, err
		}
		resolved, err := validateUploadPaths(paths, allowed)
		if err != nil {
			return kind, nil, err
		}
		return kind, resolved, t.setFiles(ctx, el, resolved)
	case kindCombobox:
		if !v.isStr {
			return kind, nil, fmt.Errorf("combobox needs a string value, got %s", v)
		}
		shown, err := t.pickCombobox(ctx, frame, el, v.str)
		if err == nil {
			*confirmed = shown
		}
		return kind, nil, err
	default:
		return kind, nil, fmt.Errorf("unhandled field kind %q", kind)
	}
}

// readBackField reads a field's current state through the form kit and
// compares it with what the fill verifiably produced (confirmed), falling back
// to the request when there is nothing confirmed.
func (t *Target) readBackField(ctx context.Context, frame, selector, kind, confirmed string, v formValue, refs *RefTable) (string, bool, error) {
	el, err := t.resolveInteract(ctx, frame, selector, refs)
	if err != nil {
		return "", false, err
	}
	raw, err := t.callKitIn(ctx, el.contextID, "readField", el.path, kind)
	if err != nil {
		return "", false, err
	}
	return compareField(kind, el.probe.Tag, confirmed, v, raw)
}

// formValue is a decoded fill-form value.
type formValue struct {
	null  bool
	isB   bool
	b     bool
	isStr bool
	str   string
	list  []string // JSON array of strings (file uploads)
}

func (v formValue) String() string {
	switch {
	case v.null:
		return "null"
	case v.isB:
		return fmt.Sprintf("%t", v.b)
	case v.isStr:
		return v.str
	default:
		return strings.Join(v.list, ", ")
	}
}

// decodeFormValue accepts null, a bool, a string, a number (kept as its literal
// text, e.g. 1.50) or an array of strings.
func decodeFormValue(raw json.RawMessage) (formValue, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return formValue{}, errors.New("missing value")
	}
	switch trimmed[0] {
	case 'n':
		if string(trimmed) != "null" {
			return formValue{}, fmt.Errorf("invalid value %s", trimmed)
		}
		return formValue{null: true}, nil
	case 't', 'f':
		var b bool
		if err := json.Unmarshal(trimmed, &b); err != nil {
			return formValue{}, fmt.Errorf("invalid value %s: %w", trimmed, err)
		}
		return formValue{isB: true, b: b}, nil
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return formValue{}, fmt.Errorf("invalid string value %s: %w", trimmed, err)
		}
		return formValue{isStr: true, str: s}, nil
	case '[':
		var list []string
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return formValue{}, fmt.Errorf("array values must be arrays of strings (file paths): %w", err)
		}
		if len(list) == 0 {
			return formValue{}, errors.New("empty array value")
		}
		return formValue{list: list}, nil
	case '{':
		return formValue{}, errors.New("object values are not supported")
	default:
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil {
			return formValue{}, fmt.Errorf("invalid value %s: %w", trimmed, err)
		}
		return formValue{isStr: true, str: n.String()}, nil
	}
}

// formPaths returns the file paths a file-field value names: a single string or
// an array of strings. Validation is the caller's job (validateUploadPaths).
func formPaths(v formValue) ([]string, error) {
	switch {
	case v.isStr:
		return []string{v.str}, nil
	case v.list != nil:
		return v.list, nil
	default:
		return nil, fmt.Errorf("file input needs a path or an array of paths, got %s", v)
	}
}

// normalizeTextForTag applies the same value sanitization the browser applies
// to a text field, on BOTH sides, so a fill the browser itself rewrites is not
// reported as a mismatch. <textarea> canonicalizes line endings to LF; <input>
// strips newlines entirely (HTML value sanitization). Other sanitizations (an
// unparseable date becoming "") are deliberately NOT normalized away: there the
// fill genuinely did not land and the mismatch is the point.
func normalizeTextForTag(tag, s string) string {
	if tag == "textarea" {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		return strings.ReplaceAll(s, "\r", "\n")
	}
	s = strings.ReplaceAll(s, "\r\n", "")
	s = strings.ReplaceAll(s, "\n", "")
	return strings.ReplaceAll(s, "\r", "")
}

// compareField renders the read-back value and decides whether it matches.
// confirmed is what applyField verified the fill produced ("" when the fill
// failed or the kind has nothing to confirm); v is the raw request, used only
// as a fallback. tag is the matched element's tag name.
func compareField(kind, tag, confirmed string, v formValue, raw json.RawMessage) (string, bool, error) {
	switch kind {
	case kindText:
		var got string
		if err := json.Unmarshal(raw, &got); err != nil {
			return "", false, fmt.Errorf("decoding value: %w", err)
		}
		return got, normalizeTextForTag(tag, got) == normalizeTextForTag(tag, v.str), nil
	case kindSelect:
		var got selectOption
		if err := json.Unmarshal(raw, &got); err != nil {
			return "", false, fmt.Errorf("decoding <select> value: %w", err)
		}
		expected, fallback := confirmed, confirmed == ""
		if fallback {
			expected = v.str
		}
		// Exact (normalized) equality with what the select verified, never a
		// substring match: an option whose text merely contains the request is a
		// different option, and an empty request must not pass at all.
		ok := normOption(got.Text) == normOption(expected)
		if fallback && got.Value == v.str {
			ok = true
		}
		return fmt.Sprintf("%s (value %q)", got.Text, got.Value), ok, nil
	case kindCheckbox, kindRadio:
		var got bool
		if err := json.Unmarshal(raw, &got); err != nil {
			return "", false, fmt.Errorf("decoding checked state: %w", err)
		}
		return fmt.Sprintf("%t", got), got == v.b, nil
	case kindFile:
		var got []string
		if err := json.Unmarshal(raw, &got); err != nil {
			return "", false, fmt.Errorf("decoding file list: %w", err)
		}
		want := v.list
		if v.isStr {
			want = []string{v.str}
		}
		ok := len(got) == len(want)
		for i := 0; ok && i < len(want); i++ {
			ok = got[i] == filepath.Base(want[i])
		}
		return strings.Join(got, ", "), ok, nil
	case kindCombobox:
		var got string
		if err := json.Unmarshal(raw, &got); err != nil {
			return "", false, fmt.Errorf("decoding combobox value: %w", err)
		}
		expected := confirmed
		if expected == "" {
			expected = v.str
		}
		// Exact (normalized) equality with what pick verified. strings.Contains
		// must not be used: strings.Contains(x, "") is always true, so an empty
		// request, or any substring of the label, would pass for a field that
		// never changed.
		return got, normOption(got) == normOption(expected), nil
	default:
		return "", false, fmt.Errorf("unknown field kind %q", kind)
	}
}
