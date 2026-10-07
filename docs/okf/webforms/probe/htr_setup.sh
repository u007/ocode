#!/bin/bash
# Create the ISOLATED htrcli home + headless Chrome (CDP :9555, 1280x1600 window) the probe drives.
# Never touches ~/.htrcli or the Chrome on :9333.
SP=$(cd "$(dirname "$0")" && pwd); HTR_HOME=${HTR_HOME:-$SP/htrhome}; H=${HTRCLI_BIN:-$(command -v htrcli)}
mkdir -p "$HTR_HOME"
printf '#!/bin/sh\nexec "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --window-size=1280,1600 "$@"\n' > "$HTR_HOME/chrome-big.sh"; chmod +x "$HTR_HOME/chrome-big.sh"
HOME=$HTR_HOME $H config set-cdp-port 9555 >/dev/null
HOME=$HTR_HOME $H config set-chrome-path "$HTR_HOME/chrome-big.sh" >/dev/null
HOME=$HTR_HOME $H browser status 2>/dev/null | grep -q "running on port" || HOME=$HTR_HOME $H browser start --headless
HOME=$HTR_HOME $H browser status | head -2
