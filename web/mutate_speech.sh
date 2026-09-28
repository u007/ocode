#!/bin/bash
# Mutation harness for SpeechProvider's speech-summary wiring.
# Each mutation must make at least one test FAIL. A surviving mutation is a
# test gap, not a pass.
set -u
cd "$(dirname "$0")"
SRC=src/components/Speech/SpeechProvider.tsx
BAK=/tmp/speechprovider.bak
cp "$SRC" "$BAK"

restore() { cp "$BAK" "$SRC"; }

run() { npx vitest run src/components/Speech/SpeechProvider.speechSummary.test.tsx 2>&1 | grep -cE "^ FAIL|Tests .*failed"; }

mutate() {
  local label="$1" old="$2" new="$3"
  python3 - "$SRC" "$old" "$new" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()
old, new = sys.argv[2], sys.argv[3]
assert s.count(old) == 1, f"anchor not unique/found: {old!r}"
p.write_text(s.replace(old, new))
PY
  local out
  out=$(run)
  restore
  if [ "$out" -gt 0 ]; then
    echo "CAUGHT   $label"
  else
    echo "SURVIVED $label  <-- test gap"
  fi
}

echo "=== M-P1: empty summary no longer falls back to the full text ==="
mutate "empty-summary fallback" \
  'return trimmed || text;' \
  'return trimmed;'

echo "=== M-P2: the full-text opt-out is ignored ==="
mutate "speakMode === full bypass" \
  'if (!summaryEnabled || speakMode === "full" || !sessionId) return text;' \
  'if (!summaryEnabled || !sessionId) return text;'

echo "=== M-P3: an unbound session no longer bypasses ==="
mutate "missing-session bypass" \
  'if (!summaryEnabled || speakMode === "full" || !sessionId) return text;' \
  'if (!summaryEnabled || speakMode === "full") return text;'

echo "=== M-P4: the server-side gate is ignored ==="
mutate "summaryEnabled gate" \
  'if (!summaryEnabled || speakMode === "full" || !sessionId) return text;' \
  'if (speakMode === "full" || !sessionId) return text;'

echo "=== M-P5: the host is dropped from the summary request ==="
mutate "host threading" \
  'await api.summarizeSpeech(sessionId, text, host);' \
  'await api.summarizeSpeech(sessionId, text, undefined);'

echo "=== M-P6: a summariser failure silences speech instead of falling back ==="
mutate "failure fallback" \
  '        console.warn("speech summary failed; speaking full text", err);
        return text;' \
  '        console.warn("speech summary failed; speaking full text", err);
        return "";'

echo "=== M-P7: a config-load failure silently turns the feature off ==="
mutate "config load fallback" \
  'console.warn("speech summary config load failed", err);' \
  'console.warn("speech summary config load failed", err);
        if (!cancelled) setSummaryConfig({ model: "", enabled: false });'

echo "=== restore + confirm green ==="
restore
npx vitest run src/components/Speech/ 2>&1 | grep -E "Tests |Test Files"
