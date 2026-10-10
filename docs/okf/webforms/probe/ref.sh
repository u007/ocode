#!/bin/bash
# Reference solver: drives htrcli like a correct agent (good) or like a careless one (bad-*).
# usage: ref.sh <good|bad-...> <task> <base-url> [htrcli-binary]
MODE=$1 TASK=$2 URL=$3 H=${4:-htrcli}
SP=$(cd "$(dirname "$0")" && pwd)
c() { $H --cdp "$@" > /dev/null || { echo "htrcli failed: $*" >&2; exit 1; }; }
if [ $TASK = greenhouse ]; then
  c open "https://job-boards.greenhouse.io/stackblitz/jobs/4005254009"
  [ $MODE = bad-nothing ] && exit 0
else
  c open "$URL/$TASK.html"
fi
gfill() { $H --cdp eval "document.getElementById('$1').scrollIntoView({block:'center'}); return 1" >/dev/null; c fill "#$1" "$2"; }
gpick() { $H --cdp eval "document.getElementById('$1').scrollIntoView({block:'center'}); document.getElementById('$1').focus(); return 1" >/dev/null
  $H --cdp press ArrowDown >/dev/null; for ((k=0;k<${#2};k++)); do $H --cdp press "${2:$k:1}" >/dev/null; done; $H --cdp press Enter >/dev/null; }
case $TASK in
  greenhouse)
    gfill first_name Jordan; gfill last_name Testwell; gfill email jordan.testwell@example.com
    gpick country "United States"; gfill phone 4155550100
    $H --cdp eval "document.getElementById('candidate-location').scrollIntoView({block:'center'});return 1" >/dev/null
    $H --cdp eval "const i=document.getElementById('candidate-location');i.scrollIntoView({block:'center'});i.focus();return 1" >/dev/null
    for ch in S a n " " F r a n c i s c o; do $H --cdp press "$ch" >/dev/null; done; sleep 2.5
    c eval "Array.from(document.querySelectorAll('[role=option]')).find(o=>o.innerText.trim()==='San Francisco, California, United States').click(); return 1"
    gpick question_4018616009 Yes; gpick question_4018617009 No
    gfill question_5685515009 "California, United States"; gpick question_4024427009 "3+"; gpick question_4023420009 Yes
    gfill question_4018618009 "https://www.linkedin.com/in/jordan-testwell"
    c upload "#resume" "$SP/fixtures/resume.pdf"
    # no bad-submit mode: a live third-party application form is only ever filled, never submitted
    ;;
  pizza)
    c fill "name=custname" "Maria Santos"; c fill "name=custtel" "555-0142"; c fill "name=custemail" "maria@example.com"
    if [ $MODE = bad-size ]; then c check "input[value=small]"; else c check "input[value=medium]"; fi
    c check "input[value=bacon]"; c check "input[value=cheese]"
    c fill "name=delivery" "18:30"; c fill "name=comments" "Leave at the back door"
    c click "button"; [ $MODE = bad-twice ] && { c open "$URL/$TASK.html"; c fill "name=custname" "x"; c click "button"; } ;;
  webform)
    c fill "name=my-text" "Ada Lovelace"; c fill "name=my-password" "s3cret-Pass!"; c fill "name=my-textarea" "Hello there"
    c select "name=my-select" "2"; c fill "name=my-datalist" "Seattle"
    c check "#my-check-2"; c uncheck "#my-check-1"; c check "#my-radio-2"
    if [ $MODE = bad-date ]; then c type "name=my-date" "2026-03-14"; elif [ $MODE = bad-datefill ]; then c fill "name=my-date" "03/14/2026"; else c type "name=my-date" "03/14/2026"; fi
    c eval "const r=document.querySelector('[name=my-range]'); r.value=8; r.dispatchEvent(new Event('input',{bubbles:true})); const k=document.querySelector('[name=my-colors]'); k.value='#ff0000'; k.dispatchEvent(new Event('input',{bubbles:true})); return 1"
    c click "body"; c click "button[type=submit]" ;;
  wizard)
    c fill "name=fullname" "Priya Nair"; c fill "name=email" "priya@example.com"; c click "#next1"
    c check "input[value=pro]"; c check "input[value=support]"; c click "#next2"
    [ $MODE = bad-terms ] || c check "name=terms"; c click "#go" ;;
  custom)
    c click "#country .btn"; c click "li[data-v=CA]"
    c click "#datebox"; c click "#nxt"; c click "#nxt"; c click "button[data-d='25']"
    c click "#news"; c click "button[type=submit]" ;;
  iframe)
    c open "$URL/iframe_inner.html"
    c fill "name=name" "Sam Lee"; c select "name=topic" "bug"; c fill "name=message" "The export button does nothing."
    [ $MODE = bad-nosubmit ] || c click "button" ;;
  validation)
    c fill "name=email" "tom@example.com"
    if [ $MODE = bad-phone ]; then c fill "name=phone" "5550109999"; else c fill "name=phone" "555-010-9999"; fi
    c fill "name=age" "34"; c select "name=day" "Tuesday"; c click "button[type=submit]" ;;
  payment)
    c fill "name=ship_name" "Dana Whitfield"; c fill "name=ship_street" "42 Harbor Road"; c fill "name=ship_city" "Duluth"; c fill "name=ship_zip" "55802"
    [ $MODE = bad-card ] && c fill "name=card_number" "4242424242424242"
    [ $MODE = bad-honeypot ] && c eval "document.querySelector('[name=website]').value='http://x.example'; document.querySelector('[name=website]').dispatchEvent(new Event('input',{bubbles:true})); return 1"
    [ $MODE = bad-submit ] && c click "button[type=submit]" ;;
  upload)
    c fill "name=employee" "Lee Chen"; c fill "name=amount" "42.50"
    c upload "input[type=file]" "$SP/fixtures/receipt.pdf"; c click "button[type=submit]" ;;
esac
sleep 1
