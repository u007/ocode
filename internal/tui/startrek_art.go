package tui

import (
	"math/rand"
	"strings"

	"charm.land/lipgloss/v2"
)

// LCARS / Star Trek art shown as the empty-state background when the lcars
// theme is active. A random variant is chosen once per session.

const startrekArt1 = `
                    ________________
               ____/  NCC-1701-D    \____
          ____/__________________________\____
         /   ____    ____    ____    ____     \
        /___/____\__/____\__/____\__/____\_____\
             \______________________________/
                    /_/            \_\
                   /_/              \_\
`

const startrekArt2 = `
                  xxxXRRRMMMMMMMMMMMMMMMxxx,.
              xXXRRRRRXXXVVXVVXXXXXXXRRRRRMMMRx,
            xXRRXRVVVVVVVVVVVVVVVXXXXXRXXRRRMMMMMRx.
          xXRXXXVVVVVVVVVVVVVVVVXXXXVXXXXXXRRRRRMMMMMxx.
        xXRRXXVVVVVttVtVVVVVVVVVtVXVVVVXXXXXRRRRRRRMMMMMXx
      xXXRXXVVVVVtVttttttVtttttttttVXXXVXXXRXXRRRRRRRMMMMMMXx
     XRXRXVXXVVVVttVtttVttVttttttVVVVXXXXXXXXXRRRRRRRMMMMMMMMVx
    XRXXRXVXXVVVVtVtttttVtttttittVVVXXVXVXXXRXRRRRRMRRMMMMMMMMMX,
   XRRRMRXRXXXVVVXVVtttittttttttttVVVVXXVXXXXXXRRRRRMRMMMMMMMMMMM,
   XXXRRRRRXXXXXXVVtttttttttttttttttVtVXVXXXXXXXRRRRRMMMMMMMMMMMMM,
   XXXXRXRXRXXVXXVtVtVVttttttttttttVtttVXXXXXXXRRRRRMMMMMMMMMMMMMMMR
   VVXXXVRVVXVVXVVVtttititiitttttttttttVVXXXXXXRRRRRMRMMMMMMMMMMMMMMV
   VttVVVXRXVVXtVVVtttii|iiiiiiittttttttitXXXRRRRRRRRRRMMMMMMMMMMMMMM
   tiRVVXRVXVVVVVit|ii||iii|||||iiiiiitiitXXXXXXXXRRRRRRMMMMMMMMMMMMM
    +iVtXVttiiii|ii|+i+|||||i||||||||itiiitVXXVXXXRRRRRRRRMMMMMMRMMMX
    `+"`"+`+itV|++|tttt|i|+||=+i|i|iiii|iiiiiiiitiVtti+++++|itttRRRRRMVXVit
     +iXV+iVt+,tVit|+=i|||||iiiiitiiiiiiii|+||itttti+=++|+iVXVRV:,|t
     +iXtiXRXXi+Vt|i||+|++itititttttttti|iiiiitVt:.:+++|+++iXRMMXXMR
     :iRtiXtiV||iVVt||||++ttittttttttttttttXXVXXRXRXXXtittt|iXRMMXRM
      :|t|iVtXV+=+Xtti+|++itiiititittttVttXXXXXXXRRRXVtVVtttttRRMMMM|
        +iiiitttt||i+++||+++|iiiiiiiiitVVVXXRXXXRRRRMXVVVVttVVVXRMMMV
         :itti|iVttt|+|++|++|||iiiiiiiittVVXRRRMMMMMMRVtitittiVXRRMMMV
           `+"`"+`i|iitVtXt+=||++++|++++|||+++iiiVVXVRXRRRV+=|tttttttiRRRMMM|
             i+++|+==++++++++++++++|||||||||itVVVViitt|+,,+,,=,+|itVX'
              |+++++.,||+|++++=+++++++|+|||||iitt||i||ii||||||itXt|
              t||+++,.=i+|+||+++++++++++++|i|ittiiii|iiitttttXVXRX|
              :||+++++.+++++++++|++|++++++|||iii||+:,:.-+:+|iViVXV
              iii||+++=.,+=,=,==++++++++++|||itttt|itiittXRXXXitV'
             ;tttii||++,.,,,.,,,,,=++++++++++|iittti|iiiiVXXXXXXV
            tVtttiii||++++=,,.  . ,,,=+++++++|itiiiiiii||||itttVt
           tVVttiiiii||||++++==,. ..,.,+++=++iiiiiitttttVVXXRRXXV
        ..ttVVttitttii||i|||||+|+=,.    .,,,,==+iittVVVXRRMXRRRV
..'''ittitttttitVttttiiiiii|ii|++++=+=..... ,.,,||+itiVVXXVXV
      ,|iitiiitttttttiiiii||ii||||||||+++++,.i|itVt+,,=,==.........
        ,|itiiiVtVtiii||iiiiii|||||||++||||tt|VXXRX|  ....  ..     ' ' ' ' '
          ,,i|ii||i||+|i|i|iiiiiiii||||ittRVVXRXRMX+, .  ...   .         ,
    .       .,+|++|||||ii|i|iiiitttVVttXVVXVXRRRRXt+. .....  . .       ,. .
  . .          ,,++|||||||i|iiitVVVXXXXVXXVXXRRRV+=,.....  ....  ..       ..
                  .,,++|||i|iittXXXXRMViRXXXXRVt+=, ..    ...... .        ..
                   ,XX+.=+++iitVVXXXRXVtXXVRRV++=,..... .,, .              .
            ....       +XX+|i,,||tXRRRXVXti|+++,,. .,,. . . .. .      . ....
  . .          .      ..  .(C):JE:.....++,,..,...,.... ..             .. ...

                Captain Jean-Luc Picard
            https://asciiart.website/art/4202

`

const startrekArt3 = `
+----------------------------------------+
| LCARS  | STARFLEET SYSTEMS ONLINE      |
|--------+-------------------------------|
| NAV    | ||||||||||||||||||...... 78%  |
| COMMS  | ||||||||||||||........... 52% |
| POWER  | |||||||||||||||||||||.... 91% |
+----------------------------------------+
`

const startrekArt4 = `
Gunther Feuereisen Commander, Starfleet First Officer
USS Atlantia, NCC-1171 Galaxy Exploration Command
                  .
                 .:.
                .:::.
               .:::::.
           ***.:::::::.***
      *******.:::::::::.*******
    ********.:::::::::::.********
   ********.:::::::::::::.********
   *******.::::::'***`+"`"+`::::.*******
   ******.::::'*********`+"`"+`::.******
    ****.:::'*************`+"`"+`:.****
      *.::'*****************`+"`"+`.*
      .:'  ***************    .
     .
`

const startrekArt5 = `
__________________           __
\_________________|)____.---'--`+"`"+`---.____
              ||    \----.________.----/
              ||     / /    `+"`"+`--\'
            __||____/ /_
           |___         \
               `+"`"+`--------'
         USS Enterprise NCC1701
`

var allStartrekArts [][]string

func init() {
	arts := []string{startrekArt1, startrekArt2, startrekArt3, startrekArt4, startrekArt5}
	allStartrekArts = make([][]string, len(arts))
	for i, art := range arts {
		allStartrekArts[i] = strings.Split(strings.Trim(art, "\n"), "\n")
	}
}

// RandomStartrekArt returns a randomly-selected LCARS art variant as a slice
// of lines. Call this once per session to get a fresh random variant.
func RandomStartrekArt() []string {
	return allStartrekArts[rand.Intn(len(allStartrekArts))]
}

// renderStartrekBackground returns the selected LCARS art centered inside a box
// of (width × height) cells. artStyle controls the foreground colour — pass
// m.styles.Text so the art uses the current theme's colour instead of the
// package-level dimStyle (pinned to tokyonight).
func renderStartrekBackground(artLines []string, width, height int, artStyle lipgloss.Style) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	visibleArt := artLines
	if len(visibleArt) > height {
		start := (len(visibleArt) - height) / 2
		visibleArt = visibleArt[start : start+height]
	}

	artW := 0
	for _, l := range visibleArt {
		if w := lipgloss.Width(l); w > artW {
			artW = w
		}
	}
	artH := len(visibleArt)

	topPad := (height - artH) / 2
	if topPad < 0 {
		topPad = 0
	}

	var sb strings.Builder
	for i := 0; i < topPad; i++ {
		sb.WriteByte('\n')
	}

	leftPad := (width - artW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	pad := strings.Repeat(" ", leftPad)

	for _, line := range visibleArt {
		sb.WriteString(pad)
		sb.WriteString(artStyle.Render(line))
		sb.WriteByte('\n')
	}

	return sb.String()
}
