package cdp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// uploadMaxFiles caps the files one upload sets. More is refused, not truncated.
const uploadMaxFiles = 50

// UploadFiles sets files on the <input type=file> that target names (an @eN ref
// or a selector) with DOM.setFileInputFiles. No file chooser opens and no file
// content is read into memory: Chrome reads the files from the paths.
//
// allowed is the caller's path policy and is required. It is called for every
// path before any CDP call, and a non-nil error aborts the whole upload.
func (t *Target) UploadFiles(ctx context.Context, frame, target string, paths []string, refs *RefTable, allowed func(path string) error) (string, error) {
	resolved, err := validateUploadPaths(paths, allowed)
	if err != nil {
		return "", err
	}
	el, err := t.resolveInteract(ctx, frame, target, refs)
	if err != nil {
		return "", err
	}
	if err := t.setFiles(ctx, el, resolved); err != nil {
		return "", err
	}
	names := make([]string, len(resolved))
	for i, p := range resolved {
		names[i] = filepath.Base(p)
	}
	return fmt.Sprintf("uploaded %d file(s) to %s: %s", len(paths), clipText(target, interactTextMax),
		clipText(strings.Join(names, ", "), interactTextMax)), nil
}

// validateUploadPaths checks every path before any of them reaches the browser
// and returns the paths to send. Each must be absolute (Chrome resolves a
// relative path against its own working directory, not the caller's). Symlinks
// are resolved first, and the caller's policy and the browser both get the
// resolved path, so a link inside an allowed root cannot name a file outside
// it. The resolved path must be an existing regular file. Globs are not expanded.
func validateUploadPaths(paths []string, allowed func(path string) error) ([]string, error) {
	if allowed == nil {
		return nil, errors.New("upload: nil allowed callback; the caller must supply the upload path policy")
	}
	if len(paths) == 0 {
		return nil, errors.New("upload: no files given")
	}
	if len(paths) > uploadMaxFiles {
		return nil, fmt.Errorf("upload: %d files exceeds the cap of %d", len(paths), uploadMaxFiles)
	}
	resolved := make([]string, len(paths))
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("upload: %s is not an absolute path", clipText(p, interactTextMax))
		}
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			return nil, fmt.Errorf("upload: %s: %s", clipText(p, interactTextMax), clipText(err.Error(), interactErrMax))
		}
		if err := allowed(r); err != nil {
			return nil, fmt.Errorf("upload refused for %s: %w", clipText(r, interactTextMax), err)
		}
		info, err := os.Stat(r)
		if err != nil {
			return nil, fmt.Errorf("upload: %s: %s", clipText(r, interactTextMax), clipText(err.Error(), interactErrMax))
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("upload: %s is not a regular file", clipText(r, interactTextMax))
		}
		resolved[i] = r
	}
	return resolved, nil
}

// setFiles sets the files on the <input type=file> el, then reads the input's
// file names back. The input is not waited on for visibility: file inputs are
// often hidden behind a styled button, and setFileInputFiles does not need them
// visible. paths must already have passed validateUploadPaths.
func (t *Target) setFiles(ctx context.Context, el interactTarget, paths []string) error {
	if el.probe.Tag != "input" || el.probe.Type != "file" {
		return fmt.Errorf("upload %s: element is <%s>, not an <input type=file>", clipText(el.raw, interactTextMax), el.probe.Tag)
	}
	if len(paths) > 1 && !el.probe.Multiple {
		return fmt.Errorf("upload %s: the input takes one file (no multiple attribute); got %d", clipText(el.raw, interactTextMax), len(paths))
	}
	if err := t.conn.Call(ctx, t.sessionID, "DOM.setFileInputFiles", map[string]any{
		"backendNodeId": el.backendID,
		"files":         paths,
	}, nil); err != nil {
		return fmt.Errorf("upload %s: DOM.setFileInputFiles: %w", clipText(el.raw, interactTextMax), err)
	}
	raw, err := t.callKitIn(ctx, el.contextID, "readField", el.path, kindFile)
	if err != nil {
		return err
	}
	actual, ok, err := compareField(kindFile, el.probe.Tag, "", formValue{list: paths}, raw)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("upload %s: the input reads %q after setting the files", clipText(el.raw, interactTextMax), clipText(actual, interactTextMax))
	}
	return nil
}
