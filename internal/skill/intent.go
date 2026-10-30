package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// IntentPackageName is the npm package behind `/plugin install @tanstack/intent`.
// Its skills live in meta/<name>/ inside the published tarball.
const IntentPackageName = "@tanstack/intent"

// intentRegistry is the npm registry the latest package is read from. It is a
// var only so tests can point it at a local server.
var intentRegistry = "https://registry.npmjs.org"

const (
	intentTimeout    = 2 * time.Minute
	intentMaxJSON    = 1 << 20
	intentMaxTarball = 32 << 20
	intentMaxFile    = 1 << 20
	intentMetaPrefix = "package/meta/"
)

// IntentSkill is one skill shipped in @tanstack/intent's meta/ directory. Its
// whole directory is kept because SKILL.md links to its references/ files and
// to sibling skills (../generate-skill/SKILL.md), so the layout must survive.
type IntentSkill struct {
	Name        string            // directory name under meta/
	Description string            // SKILL.md frontmatter description, whitespace collapsed
	Files       map[string][]byte // slash-separated path inside the skill dir -> contents
}

// IntentPackage is one published @tanstack/intent release. Skills are sorted by name.
type IntentPackage struct {
	Version string
	Skills  []IntentSkill
}

type intentRegistryLatest struct {
	Version string `json:"version"`
	Dist    struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

// FetchIntentPackage downloads the latest @tanstack/intent tarball, verifies
// its sha512 SRI integrity against the registry metadata, and extracts the
// skills under meta/. Nothing from the package is executed. ctx cancels both
// requests, so a caller that goes away does not hold a connection for the
// full client timeout.
func FetchIntentPackage(ctx context.Context) (IntentPackage, error) {
	client := &http.Client{Timeout: intentTimeout}

	metaBody, err := httpGetLimited(ctx, client, intentRegistry+"/@tanstack%2fintent/latest", intentMaxJSON)
	if err != nil {
		return IntentPackage{}, fmt.Errorf("read registry metadata: %w", err)
	}
	var latest intentRegistryLatest
	if err := json.Unmarshal(metaBody, &latest); err != nil {
		return IntentPackage{}, fmt.Errorf("parse registry metadata: %w", err)
	}

	tarball, err := httpGetLimited(ctx, client, latest.Dist.Tarball, intentMaxTarball)
	if err != nil {
		return IntentPackage{}, fmt.Errorf("download %s: %w", latest.Dist.Tarball, err)
	}
	if err := verifySRISHA512(tarball, latest.Dist.Integrity); err != nil {
		return IntentPackage{}, fmt.Errorf("%s@%s: %w", IntentPackageName, latest.Version, err)
	}

	skills, err := extractIntentSkills(tarball)
	if err != nil {
		return IntentPackage{}, fmt.Errorf("%s@%s: %w", IntentPackageName, latest.Version, err)
	}
	return IntentPackage{Version: latest.Version, Skills: skills}, nil
}

func httpGetLimited(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", url, limit)
	}
	return body, nil
}

// verifySRISHA512 checks raw against an npm integrity value ("sha512-<base64>").
// Anything else is refused: an unverifiable tarball is not installed.
func verifySRISHA512(raw []byte, integrity string) error {
	encoded, ok := strings.CutPrefix(integrity, "sha512-")
	if !ok {
		return fmt.Errorf("registry integrity %q is not sha512", integrity)
	}
	want, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode registry integrity: %w", err)
	}
	sum := sha512.Sum512(raw)
	if !bytes.Equal(sum[:], want) {
		return errors.New("tarball does not match registry integrity")
	}
	return nil
}

// extractIntentSkills reads meta/<dir>/ from a gzipped tarball. A directory is
// a skill only when it holds SKILL.md (meta/templates/ does not). Any entry
// with a traversal segment, or a link, is an error, not something to skip.
func extractIntentSkills(tgz []byte) ([]IntentSkill, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, fmt.Errorf("open tarball: %w", err)
	}
	tr := tar.NewReader(gz)
	dirs := map[string]map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tarball: %w", err)
		}
		if !strings.HasPrefix(hdr.Name, intentMetaPrefix) {
			continue
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("unsupported entry %q (type %q) under meta/", hdr.Name, hdr.Typeflag)
		}
		parts := strings.Split(strings.TrimPrefix(hdr.Name, intentMetaPrefix), "/")
		for _, p := range parts {
			if p == "" || p == "." || p == ".." || strings.Contains(p, "\\") {
				return nil, fmt.Errorf("unsafe entry path %q", hdr.Name)
			}
		}
		if len(parts) < 2 {
			continue
		}
		if hdr.Size > intentMaxFile {
			return nil, fmt.Errorf("%s is larger than %d bytes", hdr.Name, intentMaxFile)
		}
		body, err := io.ReadAll(io.LimitReader(tr, intentMaxFile+1))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		if int64(len(body)) > intentMaxFile {
			return nil, fmt.Errorf("%s is larger than %d bytes", hdr.Name, intentMaxFile)
		}
		skillDir := parts[0]
		if dirs[skillDir] == nil {
			dirs[skillDir] = map[string][]byte{}
		}
		dirs[skillDir][strings.Join(parts[1:], "/")] = body
	}

	var skills []IntentSkill
	for name, files := range dirs {
		body, ok := files["SKILL.md"]
		if !ok {
			continue
		}
		desc, err := intentDescription(body)
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", name, err)
		}
		skills = append(skills, IntentSkill{Name: name, Description: desc, Files: files})
	}
	if len(skills) == 0 {
		return nil, errors.New("no skills found under meta/")
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// intentDescription reads the description field from a SKILL.md frontmatter block.
func intentDescription(body []byte) (string, error) {
	rest, ok := strings.CutPrefix(string(body), "---\n")
	if !ok {
		return "", errors.New("SKILL.md has no frontmatter")
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", errors.New("SKILL.md frontmatter is not closed")
	}
	var fm struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return "", fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}
	return strings.Join(strings.Fields(fm.Description), " "), nil
}

// InstallIntentSkillsToClaude writes the named skills into ~/.claude/skills,
// the same target `/plugin install <skill>` uses. It is what the picker runs.
func InstallIntentSkillsToClaude(pkg IntentPackage, names []string) (string, error) {
	dir, err := claudeSkillsDir()
	if err != nil {
		return "", err
	}
	out, err := installIntentSkillsTo(pkg, names, dir)
	if err != nil {
		return "", err
	}
	// Skills changed on disk: drop the load cache so /<skill> sees the new copy.
	InvalidateSkillCache()
	return out, nil
}

// installIntentSkillsTo is InstallIntentSkillsToClaude with the target dir
// injected. Unknown names fail before anything is written; per-skill write
// failures are reported inline so the skills that did install stay visible.
func installIntentSkillsTo(pkg IntentPackage, names []string, dir string) (string, error) {
	byName := make(map[string]IntentSkill, len(pkg.Skills))
	for _, s := range pkg.Skills {
		byName[s.Name] = s
	}
	selected := make([]IntentSkill, 0, len(names))
	for _, name := range names {
		s, ok := byName[name]
		if !ok {
			return "", fmt.Errorf("@tanstack/intent %s has no skill %q", pkg.Version, name)
		}
		selected = append(selected, s)
	}
	lines := make([]string, 0, len(selected))
	for _, s := range selected {
		line, err := installIntentSkill(s, dir)
		if err != nil {
			line = fmt.Sprintf("! %s: %v", s.Name, err)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

// installIntentSkill writes one skill directory. A symlinked target is refused,
// an identical copy is left alone, and a differing file is moved aside to
// "<file>.bak.<timestamp>" before it is replaced.
func installIntentSkill(s IntentSkill, dir string) (string, error) {
	target := filepath.Join(dir, s.Name)
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := writeIntentFiles(s, target); err != nil {
			return "", err
		}
		return fmt.Sprintf("installed %s in %s", s.Name, dir), nil
	case err != nil:
		return "", err
	case info.Mode()&os.ModeSymlink != 0:
		return "", fmt.Errorf("%s is a symlink; refusing to overwrite, remove the symlink first", target)
	case !info.IsDir():
		return "", fmt.Errorf("%s exists but is not a directory", target)
	}

	var changed []string
	for rel, body := range s.Files {
		cur, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			changed = append(changed, rel)
		case err != nil:
			return "", err
		case !bytes.Equal(cur, body):
			changed = append(changed, rel)
		}
	}
	if len(changed) == 0 {
		return fmt.Sprintf("%s is already up to date in %s", s.Name, dir), nil
	}
	for _, rel := range changed {
		p := filepath.Join(target, filepath.FromSlash(rel))
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return "", err
		}
		if _, err := backupFile(p); err != nil {
			return "", fmt.Errorf("back up %s: %w", p, err)
		}
	}
	if err := writeIntentFiles(s, target); err != nil {
		return "", err
	}
	return fmt.Sprintf("updated %s in %s; replaced files were kept as *.bak.<timestamp>", s.Name, dir), nil
}

func writeIntentFiles(s IntentSkill, target string) error {
	for rel, body := range s.Files {
		p := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}
