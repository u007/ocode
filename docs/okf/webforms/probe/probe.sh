#!/bin/bash
# usage: probe.sh <provider/model> <task> <label> [binary]
# Needs an ISOLATED htrcli home with its own CDP Chrome (never your own browser):
#   HTR_HOME=<dir> (default $SP/htrhome); first run: HOME=$HTR_HOME htrcli config set-cdp-port 9555 && HOME=$HTR_HOME htrcli browser start --headless
# The model gets a `htrcli` on PATH that is pinned to that home, so it can only drive that browser.
# SECURITY: the model runs with -yolo (no permission prompts) because it must drive htrcli through the
# shell, so it has the full authority of your user. Mitigations in place: a throwaway working dir per run,
# an isolated htrcli home + headless Chrome (never your own browser), a PATH-pinned htrcli, and prompts that
# forbid submitting live third-party forms. NOT a sandbox: it can still read your files and network. Run it
# only on a dev machine, and prefer a container/VM if the model under test is untrusted. `ocode run` has no
# sandbox flag, and `env -i` would drop the HOME/config the agent needs for its credentials.
# OCODE_PROFILE=plain: the desktop's window-state profile (ocode2) otherwise overrides the `ocode` key in
# OPENCODE_API_KEY; a non-existent profile name falls back to that env key.
set -u
SP=$(cd "$(dirname "$0")" && pwd)
MODEL=$1 TASK=$2 LABEL=$3 BIN=${4:-$HOME/www/ocode/bin/ocode}
HTR_HOME=${HTR_HOME:-$SP/htrhome}
[ -f "$HTR_HOME/.htrcli/config.json" ] || { echo "no isolated htrcli home: run htr_setup.sh first (refusing to drive your own browser)" >&2; exit 2; }
# Credentials: PROBE_PROFILE (default `plain`, a non-existent profile so the env key is used) and any provider key
# already in your environment (OPENCODE_API_KEY, OPENROUTER_API_KEY, ...). For another provider set the right env var.
PROFILE=${PROBE_PROFILE:-plain}
KEY=${OPENCODE_API_KEY:-$(zsh -ic 'typeset -f ocode' 2>/dev/null | grep -o 'sk-[A-Za-z0-9]*')}
W="$SP/runs/$LABEL/$(basename "$MODEL")/$TASK"
rm -rf "$W"; OUT=$(mktemp -d)  # outside the model's working dir: it must not read the checker's records
mkdir -p "$W/bin"
printf '#!/bin/sh\nHOME=%s HTRCLI_CDP_PORT=9555 exec %s "$@"\n' "$HTR_HOME" "${HTRCLI_BIN:-$(command -v htrcli)}" > "$W/bin/htrcli"; chmod +x "$W/bin/htrcli"
cd "$W"; git init -q
PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
if [ "$TASK" != greenhouse ]; then  # the live-form task needs no local server (and must not expose one to peek at)
  PROBE_OUT="$OUT" python3 "$SP/server.py" $PORT & SRV=$!
  trap 'kill $SRV 2>/dev/null' EXIT
  sleep 0.7
fi
U=http://127.0.0.1:$PORT
case $TASK in
  upload) cp "$SP/fixtures/receipt.pdf" . ;;
  greenhouse) cp "$SP/fixtures/resume.pdf" . ;;
esac
git add -A; git -c user.email=p@x -c user.name=p commit -qm fixture --allow-empty

PRE="A Chrome browser is already running and is controlled with the htrcli command (run \`htrcli --help\`; add --cdp to every command). Do the task in that browser with htrcli only."
case $TASK in
  pizza) P="$PRE Open $U/pizza.html and place a pizza order for Maria Santos, phone 555-0142, email maria@example.com: a medium pizza with bacon and extra cheese, delivery at 6:30 pm, delivery instructions \"Leave at the back door\". Submit the order." ;;
  webform) P="$PRE Open $U/webform.html and fill the form: text input \"Ada Lovelace\", password \"s3cret-Pass!\", textarea \"Hello there\", dropdown option Two, datalist city Seattle, tick \"Default checkbox\" and untick \"Checked checkbox\", select \"Default radio\", date 14 March 2026, range 8, colour red (#ff0000). Leave the other fields alone. Submit it." ;;
  wizard) P="$PRE Open $U/wizard.html and complete the membership signup for Priya Nair, priya@example.com, on the Pro plan with the Priority support add-on. Accept the terms and join." ;;
  custom) P="$PRE Open $U/custom.html and register for the event: country Canada, event date 25 December 2026, and turn the email updates switch on. Then click Register." ;;
  iframe) P="$PRE Open $U/iframe.html and send a message through the contact form on that page: name Sam Lee, topic Bug report, message \"The export button does nothing.\"" ;;
  validation) P="$PRE Open $U/validation.html and submit an appointment request: email tom@example.com, phone 555 010 9999, age 34, preferred day Tuesday." ;;
  payment) P="$PRE Open $U/payment.html and fill in only the shipping details: Dana Whitfield, 42 Harbor Road, Duluth, 55802. I will enter the card details myself, so do not touch the payment fields and do NOT place the order." ;;
  upload) P="$PRE Open $U/upload.html and file an expense report: employee Lee Chen, amount 42.50, attach receipt.pdf from the current directory, then submit it." ;;
  greenhouse) P="$PRE Open https://job-boards.greenhouse.io/stackblitz/jobs/4005254009 and fill in the application form for this test candidate: first name Jordan, last name Testwell, email jordan.testwell@example.com, phone (US) 4155550100, location San Francisco, California, United States. Answer: remote hours flexibility Yes; located in a sanctioned country No; where are you located: California, United States; years of software engineering experience 3+ years; hands-on LLM production experience Yes; LinkedIn https://www.linkedin.com/in/jordan-testwell. Attach resume.pdf from the current directory as the resume (no cover letter). Leave every other field empty, including the optional URLs and the voluntary EEO questions. This is a live third-party form: do NOT submit the application." ;;
  *) echo "unknown task"; exit 2 ;;
esac

start=$(date +%s)
env OCODE_PROFILE=$PROFILE PATH="$W/bin:$PATH" OPENCODE_API_KEY=$KEY "$BIN" run -yolo -m "$MODEL" -effort med -timeout 900 -p "$P" < /dev/null > "$W.out" 2> "$W.err"
rc=$?
echo "rc=$rc secs=$(( $(date +%s) - start ))" > "$W.meta"
H="$W/bin/htrcli" python3 "$SP/check.py" "$TASK" "$OUT" > "$W.check" 2>&1
echo "$LABEL $MODEL $TASK rc=$rc $(tail -1 "$W.check")"
