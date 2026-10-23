package agent

import (
	"crypto/md5"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/config"
)

// fileRulesCheckInterval is the minimum gap between two disk checks of one
// rule file. Checks are lazy (driven by Decide), never a background poll.
const fileRulesCheckInterval = 10 * time.Second

// fileRulesNow is the clock used for the throttle; tests replace it.
var fileRulesNow = time.Now

// ruleFile is the process-wide view of one permission-defining config file: the
// global ocodeconfig.json, or one project's .ocode/settings.json. Every
// PermissionManager in the process (every session of every project) shares the
// ruleFile for a given path, so the file is hashed and parsed once per change
// rather than once per session.
//
// A change is detected by md5 of the file contents, not mtime, and only
// re-checked after fileRulesCheckInterval. A file that stops parsing keeps the
// last good rules (dropping to "no rules" would silently lift every ban) and the
// error is logged once per distinct content.
type ruleFile struct {
	path string

	lastCheck atomic.Int64  // unix nanos of the last disk check; 0 = never
	gen       atomic.Uint64 // bumped on every content change, first load included

	mu    sync.Mutex // serialises the slow path (read + hash + parse)
	hash  [md5.Size]byte
	rules atomic.Pointer[config.PermissionConfig]
}

var (
	ruleFilesMu sync.Mutex
	ruleFiles   = map[string]*ruleFile{}
)

// sharedRuleFile returns the one ruleFile for path.
func sharedRuleFile(path string) *ruleFile {
	ruleFilesMu.Lock()
	defer ruleFilesMu.Unlock()
	rf, ok := ruleFiles[path]
	if !ok {
		rf = &ruleFile{path: path}
		ruleFiles[path] = rf
	}
	return rf
}

// check refreshes the file's rules if the throttle window has passed and the
// contents changed. It is safe for concurrent use and cheap when throttled.
func (rf *ruleFile) check() {
	now := fileRulesNow()
	if last := rf.lastCheck.Load(); last != 0 && now.Sub(time.Unix(0, last)) < fileRulesCheckInterval {
		return
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Another goroutine may have refreshed while this one waited on mu.
	if last := rf.lastCheck.Load(); last != 0 && now.Sub(time.Unix(0, last)) < fileRulesCheckInterval {
		return
	}
	rf.lastCheck.Store(now.UnixNano())

	data, err := os.ReadFile(rf.path)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("permission rules: read %s failed, keeping last good rules: %v", rf.path, err)
		return
	}
	sum := md5.Sum(data) // a missing file hashes as empty content
	if rf.gen.Load() != 0 && sum == rf.hash {
		return
	}
	rf.hash = sum

	var parsed config.PermissionConfig
	if len(data) > 0 {
		parsed, err = config.ParsePermissionRules(data)
		if err != nil {
			log.Printf("permission rules: %s does not parse, keeping last good rules: %v", rf.path, err)
			// The new hash is recorded above so an unchanged broken file is not
			// re-parsed and re-logged every interval.
			return
		}
	}
	rf.rules.Store(&parsed)
	rf.gen.Add(1)
}

// fileRuleSet is the effective, validated rule set contributed by the config
// files (global merged with project).
type fileRuleSet struct {
	tools       map[string]PermissionLevel
	prefixes    map[string]PermissionLevel
	autoAllow   map[string]bool
	prefixModes map[string]string
}

func newFileRuleSet() *fileRuleSet {
	return &fileRuleSet{
		tools:       map[string]PermissionLevel{},
		prefixes:    map[string]PermissionLevel{},
		autoAllow:   map[string]bool{},
		prefixModes: map[string]string{},
	}
}

// layer merges one file's rules into s. A deny already present always wins, so a
// project file can tighten the global rules but never lift a global ban; any
// other existing level is replaced (project overrides global).
func (s *fileRuleSet) layer(cfg *config.PermissionConfig) {
	if cfg == nil {
		return
	}
	for k, v := range cfg.Tools {
		level := PermissionLevel(v)
		if !validPermissionLevel(level) || (k == "bash" && level == PermissionAllow) {
			continue
		}
		if s.tools[k] == PermissionDeny {
			continue
		}
		s.tools[k] = level
	}
	for k, v := range cfg.Bash.Prefixes {
		level := PermissionLevel(v)
		k = strings.TrimSpace(k)
		if ValidateBashPrefixRule(k, level) != nil {
			continue
		}
		if s.prefixes[k] == PermissionDeny {
			continue
		}
		s.prefixes[k] = level
	}
	for _, p := range cfg.Bash.AutoAllowPrefixes {
		p = strings.TrimSpace(p)
		if p == "" || ValidateBashPrefixRule(p, PermissionAllow) != nil || s.prefixes[p] == PermissionDeny {
			continue
		}
		s.autoAllow[p] = true
	}
	for k, v := range cfg.Bash.PrefixModes {
		v = strings.TrimSpace(v)
		if v == bashPrefixModeReadOnly || v == bashPrefixModeMutating || v == bashPrefixModeNever {
			s.prefixModes[k] = v
		}
	}
}

// projectRulePath is the project settings file for workDir, "" when there is no
// workDir.
func projectRulePath(workDir string) string {
	if workDir == "" {
		return ""
	}
	return config.ProjectSettingsPathForRoot(filepath.Clean(workDir))
}

// refreshFileRules brings this manager in line with the shared global and
// project rule files. It is called at the top of Decide; both underlying checks
// are throttled, so the common case is two atomic loads.
func (pm *PermissionManager) refreshFileRules() {
	if pm == nil {
		return
	}
	var global *ruleFile
	if path, err := config.GlobalOcodeConfigPath(); err == nil {
		global = sharedRuleFile(path)
		global.check()
	} else {
		log.Printf("permission rules: resolve global config path: %v", err)
	}
	projectPath := projectRulePath(pm.workDir)
	var project *ruleFile
	if projectPath != "" {
		project = sharedRuleFile(projectPath)
		project.check()
	}

	var globalGen, projectGen uint64
	if global != nil {
		globalGen = global.gen.Load()
	}
	if project != nil {
		projectGen = project.gen.Load()
	}

	pm.fileRulesMu.Lock()
	defer pm.fileRulesMu.Unlock()
	if pm.fileRulesApplied != nil && globalGen == pm.fileRulesGlobalGen &&
		projectGen == pm.fileRulesProjectGen && projectPath == pm.fileRulesProjectPath {
		return
	}

	next := newFileRuleSet()
	if global != nil {
		next.layer(global.rules.Load())
	}
	if project != nil {
		next.layer(project.rules.Load())
	}
	pm.applyFileRuleDelta(pm.fileRulesApplied, next)
	pm.fileRulesApplied = next
	pm.fileRulesGlobalGen = globalGen
	pm.fileRulesProjectGen = projectGen
	pm.fileRulesProjectPath = projectPath
}

// applyFileRuleDelta moves the manager from the previously applied file rules
// (nil on the first refresh) to next. Only keys the files changed are touched,
// so rules set in-session and not (yet) in a file survive a reload. A key the
// files dropped is removed only while the manager still holds the value the file
// gave it; a value the user changed in-session since is theirs to keep.
func (pm *PermissionManager) applyFileRuleDelta(prev, next *fileRuleSet) {
	if prev == nil {
		prev = newFileRuleSet()
	}
	for k, v := range next.tools {
		if prev.tools[k] != v && !pm.hasToolRule(k, v) {
			pm.SetRule(k, v)
		}
	}
	for k, v := range prev.tools {
		if _, still := next.tools[k]; !still {
			pm.removeToolRuleIf(k, v)
		}
	}
	for k, v := range next.prefixes {
		if prev.prefixes[k] != v {
			pm.SetBashPrefixRule(k, v)
		}
	}
	for k, v := range prev.prefixes {
		if _, still := next.prefixes[k]; still {
			continue
		}
		if cur, ok := pm.bashPrefixSnapshot()[k]; ok && cur == v {
			pm.RemoveBashPrefixRule(k)
		}
	}
	for p := range next.autoAllow {
		pm.bashAutoAllow.mutate(func(m map[string]bool) { m[p] = true })
		if _, ok := pm.bashPrefixModes.get(p); !ok {
			pm.bashPrefixModes.mutate(func(m map[string]string) { m[p] = bashPrefixModeReadOnly })
		}
	}
	for p := range prev.autoAllow {
		if !next.autoAllow[p] {
			pm.bashAutoAllow.mutate(func(m map[string]bool) { delete(m, p) })
		}
	}
	for k, v := range next.prefixModes {
		if prev.prefixModes[k] != v {
			pm.bashPrefixModes.mutate(func(m map[string]string) { m[k] = v })
		}
	}
	for k, v := range prev.prefixModes {
		if _, still := next.prefixModes[k]; still {
			continue
		}
		if cur, ok := pm.bashPrefixModes.get(k); ok && cur == v {
			pm.bashPrefixModes.mutate(func(m map[string]string) { delete(m, k) })
		}
	}
}

// hasToolRule reports whether tool already carries exactly level, so a re-apply
// of an unchanged wildcard rule does not append a duplicate pattern.
func (pm *PermissionManager) hasToolRule(tool string, level PermissionLevel) bool {
	if strings.Contains(tool, "*") {
		for _, p := range pm.patterns.load() {
			if p.pattern == tool && p.level == level {
				return true
			}
		}
		return false
	}
	cur, ok := pm.rules.get(tool)
	return ok && cur == level
}

// removeToolRuleIf deletes tool's rule when it still equals level.
func (pm *PermissionManager) removeToolRuleIf(tool string, level PermissionLevel) {
	if strings.Contains(tool, "*") {
		pm.patterns.mutate(func(s []patternRule) []patternRule {
			out := make([]patternRule, 0, len(s))
			for _, p := range s {
				if p.pattern == tool && p.level == level {
					continue
				}
				out = append(out, p)
			}
			return out
		})
		return
	}
	if cur, ok := pm.rules.get(tool); ok && cur == level {
		pm.rules.mutate(func(m map[string]PermissionLevel) { delete(m, tool) })
	}
}
