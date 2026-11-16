#!/usr/bin/env bash
# Create the one-time `ocode-dev` self-signed code-signing certificate.
#
# macOS keys a TCC grant (Accessibility, Screen Recording, Files & Folders) to
# the app's designated requirement. With ad-hoc signing that requirement is the
# cdhash, so every rebuild orphans the grant. Signing with a certificate makes
# it `identifier "com.u007.ocode" and certificate leaf = H"…"`, which survives
# rebuilds. Run once; scripts/bundle-macos.sh detects the identity and uses it.
set -euo pipefail

IDENTITY="ocode-dev"
KEYCHAIN="$HOME/Library/Keychains/login.keychain-db"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: macOS only (Windows and Linux need no per-app OS grant)" >&2
  exit 1
fi

# `-v` is omitted on purpose: it lists only identities chaining to a trusted
# root, so a self-signed identity would never be seen and this script would
# re-import on every run.
# Capture first, then match: `security … | grep -q` can report "not found" when
# it did match, because grep closes the pipe and `set -o pipefail` turns the
# producer's SIGPIPE into a failure.
IDENTITIES="$(security find-identity -p codesigning 2>/dev/null || true)"
if [[ "$IDENTITIES" == *"\"$IDENTITY\""* ]]; then
  echo "signing identity already present: $IDENTITY"
  exit 0
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# A failure here must say WHY. Redirecting both streams to /dev/null leaves
# `make signing-cert` exiting 1 with no cause, and the PKCS#12 step is exactly
# where a Homebrew OpenSSL and the login keychain disagree.
if ! openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout "$WORK/key.pem" -out "$WORK/cert.pem" \
  -subj "/CN=$IDENTITY" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=critical,codeSigning" \
  -addext "basicConstraints=critical,CA:false" >/dev/null 2>"$WORK/req.err"; then
  cat "$WORK/req.err" >&2
  echo "error: openssl could not create the certificate (see above)" >&2
  exit 1
fi

# OpenSSL 3 exports PKCS#12 with PBES2, which `security import` cannot read;
# LibreSSL (stock macOS) already emits the legacy algorithms.
if ! openssl pkcs12 -export -legacy -inkey "$WORK/key.pem" -in "$WORK/cert.pem" \
  -out "$WORK/cert.p12" -passout pass:ocode 2>"$WORK/p12-legacy.err"; then
  if ! openssl pkcs12 -export -inkey "$WORK/key.pem" -in "$WORK/cert.pem" \
    -out "$WORK/cert.p12" -passout pass:ocode 2>"$WORK/p12.err"; then
    cat "$WORK/p12-legacy.err" "$WORK/p12.err" >&2
    echo "error: openssl could not export a PKCS#12 the keychain accepts" >&2
    exit 1
  fi
fi

# codesign is the only allowed consumer. macOS may ask once whether codesign may
# use the key from the login keychain; answer "Always Allow".
security import "$WORK/cert.p12" -k "$KEYCHAIN" -P ocode -T /usr/bin/codesign

# Prove the key is usable now rather than on the next `make desktop-app`.
cp /bin/echo "$WORK/probe"
if ! codesign --force --sign "$IDENTITY" "$WORK/probe" 2>"$WORK/err"; then
  cat "$WORK/err" >&2
  cat >&2 <<'HELP'
error: codesign could not use the new key.
       Open Keychain Access, select the ocode-dev certificate, Get Info ->
       Access Control -> allow all applications, then rerun this script.
HELP
  exit 1
fi

echo "created signing identity: $IDENTITY"
echo "next: make desktop-app, then re-grant the OS permissions once."
echo "      Later rebuilds keep the grants."
