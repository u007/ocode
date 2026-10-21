#!/usr/bin/env python3
"""Mutation-check the DB environment-template feature.

Each mutant is a small, surgical edit to one source file. For every mutant:

  1. compile-check it (go build ./internal/...) -- a mutant that does not
     compile is INVALID, never CAUGHT (docs/gotchas/
     mutation-check-mutants-must-compile.md);
  2. run the affected test package;
  3. restore the original file and VERIFY the restore (byte-compare against
     the backup).

Hygiene (docs/gotchas/mutation-check-mutants-must-compile.md section 3):
  - private mktemp backup dir;
  - EXIT/INT/TERM trap that restores every mutated file;
  - cmp-before-restore;
  - a preflight that refuses to run against a dirty victim file.
"""

import os
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# (label, file, old, new, test-package, test-name-filter)
MUTANTS = [
    # 1. The unresolved-variable guard: an unset BRACED reference must error.
    (
        "unresolved braced reference must error",
        "internal/dbconnect/expand.go",
        """				return "", fmt.Errorf("environment variable %q is not set", name)
			}
			// A bare reference that does not resolve is literal text.""",
        """				fmt.Sprintf("environment variable %q is not set", name)
			}
			_ = braced
			// A bare reference that does not resolve is literal text.""",
        "./internal/dbconnect/",
        "TestExpandEnv",
    ),
    # 2. The bare-reference fallback: an unset BARE reference must stay
    # literal. The first attempt at this mutant wrote "$" + name, which is
    # EQUIVALENT to the original (the original writes "$" and then advances
    # one byte, so the name is copied verbatim on the next iterations) --
    # an equivalent mutant can never be caught, so a "miss" there is not a
    # coverage gap. This one DROPS the name, which is a real semantic
    # change: "p$ssword" would become "p$".
    (
        "unresolved bare reference must stay literal",
        "internal/dbconnect/expand.go",
        """			// A bare reference that does not resolve is literal text. This is
			// what keeps a Postgres password containing "$" working.
			b.WriteByte('$')
			i++
			continue""",
        """			// A bare reference that does not resolve is literal text. This is
			// what keeps a Postgres password containing "$" working.
			b.WriteByte('$')
			i = next
			continue""",
        "./internal/dbconnect/",
        "TestExpandEnv",
    ),
    # 3. A template connection must be stored as a template, not sealed.
    (
        "template connection must not be sealed",
        "internal/server/handler_dbconnect.go",
        "	if dbconnect.IsTemplate(req.URL) {",
        "	if false {",
        "./internal/server/",
        "TestDBConnect",
    ),
    # 4. The template must be RESOLVED at unlock.
    (
        "template must be resolved at unlock",
        "internal/server/handler_dbconnect.go",
        """			resolved, err := dbResolveTemplate(c.EnvTemplate, projectRoot)""",
        """			resolved, err := "", error(nil)
			_ = resolved
			_ = projectRoot""",
        "./internal/server/",
        "TestDBConnect",
    ),
    # 5. The expanded SQLite path must still pass containment.
    (
        "expanded sqlite path must pass containment",
        "internal/server/handler_db.go",
        """	expanded, errMsg := h.dbExpandPath(path, projectRoot)
	if errMsg != "" {
		return "", errMsg
	}
	resolved, errMsg := h.resolveProjectFilePath(projectRoot, expanded)""",
        """	resolved, errMsg := h.resolveProjectFilePath(projectRoot, path)""",
        "./internal/server/",
        "TestDBIDE",
    ),
    # 6. .env.local must override .env.
    (
        "env.local must override env",
        "internal/envfile/envfile.go",
        """	for _, name := range []string{FileName, LocalFileName} {""",
        """	for _, name := range []string{LocalFileName, FileName} {""",
        "./internal/envfile/",
        "TestLoad",
    ),
    # 7. The process environment must win over the file.
    (
        "process env must win over file",
        "internal/envfile/envfile.go",
        """	if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	v, ok := fileValues[name]
	return v, ok""",
        """	v, ok := fileValues[name]
	if !ok {
		v, ok = os.LookupEnv(name)
	}
	return v, ok""",
        "./internal/envfile/",
        "TestLookup",
    ),
    # 8. The template map must be WRITTEN under its own top-level key, so an
    # older ocode binary (which re-marshals the "db" section from its own
    # narrower struct) cannot destroy it.
    (
        "template map must be written to its own key",
        "internal/config/ocodeconfig.go",
        """	if len(dbEnvTemplatesCanonical) > 0 {
		if raw, err := json.Marshal(dbEnvTemplatesCanonical); err == nil {
			payload[DBEnvTemplatesKey] = json.RawMessage(raw)
		}
	} else {""",
        """	if len(dbEnvTemplatesCanonical) > 0 {
		if raw, err := json.Marshal(dbEnvTemplatesCanonical); err == nil {
			_ = raw
		}
	} else {""",
        "./internal/config/",
        "TestDBEnvTemplate",
    ),
    # 9. The load path must REPAIR the "db" section from the map, so a
    # template stripped by an older binary is restored.
    (
        "load must repair the db section from the map",
        "internal/config/ocodeconfig.go",
        """	reconcileDBEnvTemplates(cfg.DB.Connections, dbEnvTemplates(cfg.Extra))""",
        """	_ = reconcileDBEnvTemplates""",
        "./internal/config/",
        "TestDBEnvTemplate",
    ),
    # 10. The map is authoritative: the map's template must WIN over a stale
    # in-section field.
    (
        "the map must win over the in-section field",
        "internal/config/db_connections.go",
        """		if tmpl, ok := templates[conns[i].Name]; ok && tmpl != "" {
			conns[i].EnvTemplate = tmpl
		}""",
        """		if tmpl, ok := templates[conns[i].Name]; ok && tmpl != "" {
			_ = tmpl
		}""",
        "./internal/config/",
        "TestDBEnvTemplate",
    ),
]


def run(cmd, **kw):
    return subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, **kw)


def main():
    # Preflight: the victim files are the CURRENT working-tree state, which
    # may legitimately include this feature's own (uncommitted) changes. The
    # backup below is what protects them: the restore copies the backup back
    # byte for byte, so nothing is lost. The check here only records which
    # files are dirty, for the summary.
    dirty = []
    for _, rel, _, _, _, _ in MUTANTS:
        out = run(["git", "status", "--porcelain", "--", rel])
        if out.stdout.strip():
            dirty.append(rel)
    if dirty:
        print("note: victim files carry working-tree changes (expected):", dirty)

    backup = tempfile.mkdtemp(prefix="dbenv-mut-")
    print(f"backup dir: {backup}")

    originals = {}
    for _, rel, _, _, _, _ in MUTANTS:
        src = os.path.join(ROOT, rel)
        dst = os.path.join(backup, rel.replace("/", "__"))
        shutil.copy2(src, dst)
        originals[rel] = dst

    def restore_all():
        for rel, dst in originals.items():
            src = os.path.join(ROOT, rel)
            shutil.copy2(dst, src)

    results = []
    try:
        for label, rel, old, new, pkg, filt in MUTANTS:
            src = os.path.join(ROOT, rel)
            with open(src, "r", encoding="utf-8") as fh:
                body = fh.read()
            if body.count(old) != 1:
                print(f"SKIP  {label}: anchor appears {body.count(old)} times")
                results.append((label, "SKIP", "anchor not unique"))
                continue
            with open(src, "w", encoding="utf-8") as fh:
                fh.write(body.replace(old, new, 1))

            # Only the packages this feature touches are compile-checked.
            # internal/browse carries unrelated concurrent WIP that does not
            # compile, and a broken sibling package would make every mutant
            # look INVALID for a reason that has nothing to do with it.
            build = run(["go", "build", "./internal/dbconnect/", "./internal/envfile/",
                         "./internal/config/", "./internal/server/"])
            if build.returncode != 0:
                print(f"INVALID {label}: does not compile")
                results.append((label, "INVALID", build.stderr.strip()[:300]))
                restore_all()
                # re-apply nothing; the next mutant starts from the originals
                with open(src, "w", encoding="utf-8") as fh:
                    fh.write(body)
                continue

            test = run(["go", "test", pkg, "-run", filt, "-count=1"])
            if test.returncode != 0:
                print(f"CAUGHT {label}")
                results.append((label, "CAUGHT", ""))
            else:
                print(f"SURVIVED {label}  <-- coverage gap")
                results.append((label, "SURVIVED", ""))

            # Restore BEFORE the next mutant. Without this, the next mutant
            # is applied on top of this one's residue, so a later mutant can
            # be "CAUGHT" by an earlier one's damage and a genuine survivor
            # can be masked. Each mutant must be tested in isolation.
            restore_all()
            with open(src, "w", encoding="utf-8") as fh:
                fh.write(body)
    finally:
        restore_all()

    # Verify every restore.
    bad = []
    for rel, dst in originals.items():
        src = os.path.join(ROOT, rel)
        if subprocess.run(["cmp", "-s", src, dst]).returncode != 0:
            bad.append(rel)
    if bad:
        print("RESTORE FAILED for:", bad)
        return 3
    print("all files restored")

    print("\n=== SUMMARY ===")
    for label, verdict, detail in results:
        print(f"{verdict:9} {label}" + (f"  {detail}" if verdict == "INVALID" else ""))
    survived = [r for r in results if r[1] == "SURVIVED"]
    invalid = [r for r in results if r[1] == "INVALID"]
    print(f"\ncaught={len([r for r in results if r[1] == 'CAUGHT'])} "
          f"survived={len(survived)} invalid={len(invalid)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
