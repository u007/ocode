// Package wallpaper manages chat background wallpapers for the ocode web UI.
//
// Wallpapers are SVG images generated procedurally (no third-party assets).
// Each wallpaper has a stable ID used for config persistence and API lookups.
// Wallpapers are categorized (dark, light, developer, cute) and can be
// selected independently for light and dark color schemes.
package wallpaper

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/paths"
)

const (
	CategoryDark      = "dark"
	CategoryLight     = "light"
	CategoryDeveloper = "developer"
	CategoryCute      = "cute"
)

const (
	ModeAuto  = "auto"
	ModeLight = "light"
	ModeDark  = "dark"
)

var ValidModes = []string{ModeAuto, ModeLight, ModeDark}

type WallpaperMeta struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	Thumbnail  string `json:"thumbnail"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	IsBuiltin  bool   `json:"is_builtin"`
	IsUploaded bool   `json:"is_uploaded"`
	SupportsDark   bool `json:"supports_dark"`
	SupportsLight  bool `json:"supports_light"`
}

type WallpaperConfig struct {
	Enabled bool   `json:"enabled"`
	LightID string `json:"light_id"`
	DarkID  string `json:"dark_id"`
	Mode    string `json:"mode"`
}

func DefaultWallpaperConfig() WallpaperConfig {
	return WallpaperConfig{
		Enabled: false,
		Mode:    ModeAuto,
	}
}

var builtinWallpapers = map[string]WallpaperMeta{
	"dark-gradient": {
		ID: "dark-gradient", Name: "Midnight Gradient", Category: CategoryDark,
		SupportsDark: true, SupportsLight: false,
	},
	"dark-mesh": {
		ID: "dark-mesh", Name: "Mesh Dark", Category: CategoryDark,
		SupportsDark: true, SupportsLight: false,
	},
	"dark-grid": {
		ID: "dark-grid", Name: "Grid Dark", Category: CategoryDark,
		SupportsDark: true, SupportsLight: false,
	},
	"light-gradient": {
		ID: "light-gradient", Name: "Soft Light Gradient", Category: CategoryLight,
		SupportsDark: false, SupportsLight: true,
	},
	"light-pastel": {
		ID: "light-pastel", Name: "Pastel Light", Category: CategoryLight,
		SupportsDark: false, SupportsLight: true,
	},
	"light-peach": {
		ID: "light-peach", Name: "Peach Light", Category: CategoryLight,
		SupportsDark: false, SupportsLight: true,
	},
	"developer-code": {
		ID: "developer-code", Name: "Code Flow", Category: CategoryDeveloper,
		SupportsDark: true, SupportsLight: false,
	},
	"developer-terminal": {
		ID: "developer-terminal", Name: "Terminal", Category: CategoryDeveloper,
		SupportsDark: true, SupportsLight: false,
	},
	"cute-dots": {
		ID: "cute-dots", Name: "Polka Dots", Category: CategoryCute,
		SupportsDark: true, SupportsLight: true,
	},
	"cute-hearts": {
		ID: "cute-hearts", Name: "Hearts", Category: CategoryCute,
		SupportsDark: false, SupportsLight: true,
	},
	"cute-stars": {
		ID: "cute-stars", Name: "Stars", Category: CategoryCute,
		SupportsDark: true, SupportsLight: false,
	},
}

func WallpaperDir() (string, error) {
	dir, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wallpapers"), nil
}

func UserUploadDir() (string, error) {
	wd, err := WallpaperDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "user"), nil
}

func WallpaperPath(id string) (string, error) {
	if _, ok := builtinWallpapers[id]; ok {
		dir, err := WallpaperDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, id+".svg"), nil
	}
	if isUserUpload(id) {
		dir, err := UserUploadDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, id+".svg"), nil
	}
	return "", fmt.Errorf("wallpaper %q not found", id)
}

func isUserUpload(id string) bool {
	return strings.HasPrefix(id, "upload-")
}

func GenerateBuiltinWallpapers() ([]string, error) {
	dir, err := WallpaperDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	var generated []string
	for id := range builtinWallpapers {
		p := filepath.Join(dir, id+".svg")
		if _, err := os.Stat(p); err == nil {
			continue
		}
		svg := generateSVG(id)
		if err := os.WriteFile(p, []byte(svg), 0644); err != nil {
			return nil, fmt.Errorf("write wallpaper %s: %w", id, err)
		}
		generated = append(generated, id)
	}
	return generated, nil
}

func EnsureWallpaperAssets() error {
	if _, err := GenerateBuiltinWallpapers(); err != nil {
		return err
	}
	_, err := UserUploadDir()
	return err
}

func ListWallpapers() ([]WallpaperMeta, error) {
	builtins := make([]WallpaperMeta, 0, len(builtinWallpapers))
	for _, meta := range builtinWallpapers {
		path, err := WallpaperPath(meta.ID)
		if err != nil {
			continue
		}
		svg, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		meta.Thumbnail = "data:image/svg+xml;base64," + svgBase64(svg)
		meta.Width = 1920
		meta.Height = 1080
		meta.IsBuiltin = true
		builtins = append(builtins, meta)
	}
	sort.Slice(builtins, func(i, j int) bool {
		return builtins[i].Name < builtins[j].Name
	})
	uploads, err := listUploads()
	if err != nil {
		return builtins, nil
	}
	return append(builtins, uploads...), nil
}

func GetWallpaperMeta(id string) (WallpaperMeta, error) {
	if meta, ok := builtinWallpapers[id]; ok {
		path, err := WallpaperPath(id)
		if err == nil {
			svg, err := os.ReadFile(path)
			if err == nil {
				meta.Thumbnail = "data:image/svg+xml;base64," + svgBase64(svg)
			}
		}
		meta.Width = 1920
		meta.Height = 1080
		meta.IsBuiltin = true
		return meta, nil
	}
	if meta, ok := getUploadMeta(id); ok {
		return meta, nil
	}
	return WallpaperMeta{}, fmt.Errorf("wallpaper %q not found", id)
}

func ReadWallpaperFile(id string) ([]byte, error) {
	path, err := WallpaperPath(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func RegisterUserUpload(filename string, data []byte) (WallpaperMeta, error) {
	dir, err := UserUploadDir()
	if err != nil {
		return WallpaperMeta{}, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return WallpaperMeta{}, err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return WallpaperMeta{}, fmt.Errorf("generate filename: %w", err)
	}
	safeID := "upload-" + hex.EncodeToString(buf)
	path := filepath.Join(dir, safeID+".svg")
	contentType := detectContentType(data)
	if contentType == "image/svg+xml" {
		if err := os.WriteFile(path, data, 0644); err != nil {
			return WallpaperMeta{}, fmt.Errorf("write upload: %w", err)
		}
	} else {
		svg := wrapImageInSVG(data, contentType)
		if err := os.WriteFile(path, []byte(svg), 0644); err != nil {
			return WallpaperMeta{}, fmt.Errorf("write upload: %w", err)
		}
	}
	meta := WallpaperMeta{
		ID: safeID, Name: "Uploaded Image", Category: CategoryDeveloper,
		IsUploaded: true, SupportsDark: true, SupportsLight: true,
		Width: 1920, Height: 1080,
	}
	return meta, nil
}

func RemoveUserUpload(id string) error {
	path, err := WallpaperPath(id)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func listUploads() ([]WallpaperMeta, error) {
	dir, err := UserUploadDir()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var uploads []WallpaperMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".svg")
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		meta := WallpaperMeta{
			ID: id, Name: "Uploaded Image", Category: CategoryDeveloper,
			Thumbnail: "data:image/svg+xml;base64," + svgBase64(data),
			IsUploaded: true, SupportsDark: true, SupportsLight: true,
			Width: 1920, Height: 1080,
		}
		uploads = append(uploads, meta)
	}
	return uploads, nil
}

func getUploadMeta(id string) (WallpaperMeta, bool) {
	uploads, _ := listUploads()
	for _, m := range uploads {
		if m.ID == id {
			return m, true
		}
	}
	return WallpaperMeta{}, false
}

func svgBase64(data []byte) string {
	b64 := make([]byte, base64EncodedLen(len(data)))
	n := encodeBase64(b64, data)
	return string(b64[:n])
}

func base64EncodedLen(n int) int {
	return ((n + 2) / 3) * 4
}

func encodeBase64(dst, src []byte) int {
	enc := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	i := 0
	j := 0
	for i < len(src) {
		a := src[i]
		var b, c byte
		if i+1 < len(src) { b = src[i+1] }
		if i+2 < len(src) { c = src[i+2] }
		dst[j] = enc[a>>2]; j++
		dst[j] = enc[(a&0x03)<<4|(b>>4)]; j++
		if i+1 < len(src) { dst[j] = enc[(b&0x0f)<<2|(c>>6)] } else { dst[j] = '=' }
		j++
		if i+2 < len(src) { dst[j] = enc[c&0x3f] } else { dst[j] = '=' }
		j++
		i += 3
	}
	return j
}

func detectContentType(data []byte) string {
	if len(data) < 4 { return "application/octet-stream" }
	if string(data[:5]) == "<?xml" || string(data[:4]) == "<svg" { return "image/svg+xml" }
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 { return "image/png" }
	if data[0] == 0xFF && data[1] == 0xD8 { return "image/jpeg" }
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 { return "image/gif" }
	if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 { return "image/webp" }
	return "application/octet-stream"
}

func wrapImageInSVG(data []byte, contentType string) string {
	b64 := make([]byte, base64EncodedLen(len(data)))
	n := encodeBase64(b64, data)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080"><image width="1920" height="1080" href="data:%s;base64,%s"/></svg>`, contentType, string(b64[:n]))
}

func generateSVG(id string) string {
	switch id {
	case "dark-gradient": return darkGradientSVG()
	case "dark-mesh": return darkMeshSVG()
	case "dark-grid": return darkGridSVG()
	case "light-gradient": return lightGradientSVG()
	case "light-pastel": return lightPastelSVG()
	case "light-peach": return lightPeachSVG()
	case "developer-code": return developerCodeSVG()
	case "developer-terminal": return developerTerminalSVG()
	case "cute-dots": return cuteDotsSVG()
	case "cute-hearts": return cuteHeartsSVG()
	case "cute-stars": return cuteStarsSVG()
	default: return darkGradientSVG()
	}
}

func darkGradientSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%">
<stop offset="0%" stop-color="#0f0c29"/><stop offset="50%" stop-color="#302b63"/><stop offset="100%" stop-color="#24243e"/>
</linearGradient></defs>
<rect width="1920" height="1080" fill="url(#g)"/>
<circle cx="1400" cy="200" r="300" fill="#7c3aed" opacity="0.15"/>
<circle cx="400" cy="800" r="200" fill="#3b82f6" opacity="0.1"/>
</svg>`
}

func darkMeshSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<defs>
<radialGradient id="g1" cx="30%" cy="30%"><stop offset="0%" stop-color="#1e1b4b"/><stop offset="100%" stop-color="#020617"/></radialGradient>
<radialGradient id="g2" cx="70%" cy="70%"><stop offset="0%" stop-color="#1e3a5f"/><stop offset="100%" stop-color="#020617"/></radialGradient>
</defs>
<rect width="1920" height="1080" fill="url(#g1)"/>
<rect width="1920" height="1080" fill="url(#g2)" opacity="0.5"/>
<path d="M0 540 Q480 200 960 540 T1920 540" stroke="#6366f1" stroke-width="1" fill="none" opacity="0.2"/>
<path d="M0 500 Q480 840 960 500 T1920 500" stroke="#8b5cf6" stroke-width="1" fill="none" opacity="0.15"/>
<path d="M0 600 Q480 300 960 600 T1920 600" stroke="#a78bfa" stroke-width="1" fill="none" opacity="0.1"/>
</svg>`
}

func darkGridSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<rect width="1920" height="1080" fill="#0a0a0a"/>
<defs>
<pattern id="grid" width="40" height="40" patternUnits="userSpaceOnUse"><path d="M40 0H0V40" stroke="#333" stroke-width="0.5" fill="none"/></pattern>
<linearGradient id="fade" x1="0%" y1="0%" x2="0%" y2="100%"><stop offset="0%" stop-color="#1a1a2e"/><stop offset="100%" stop-color="#0a0a0a" opacity="0"/></linearGradient>
</defs>
<rect width="1920" height="1080" fill="url(#grid)" opacity="0.3"/><rect width="1920" height="1080" fill="url(#fade)"/>
</svg>`
}

func lightGradientSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%">
<stop offset="0%" stop-color="#f5f3ff"/><stop offset="50%" stop-color="#e0e7ff"/><stop offset="100%" stop-color="#fef3c7"/>
</linearGradient></defs>
<rect width="1920" height="1080" fill="url(#g)"/>
<circle cx="500" cy="300" r="250" fill="#c7d2fe" opacity="0.3"/><circle cx="1400" cy="700" r="200" fill="#fde68a" opacity="0.3"/>
</svg>`
}

func lightPastelSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%">
<stop offset="0%" stop-color="#fce4ec"/><stop offset="50%" stop-color="#e8eaf6"/><stop offset="100%" stop-color="#e0f2f1"/>
</linearGradient></defs>
<rect width="1920" height="1080" fill="url(#g)"/>
<circle cx="300" cy="400" r="300" fill="#f8bbd0" opacity="0.25"/><circle cx="1500" cy="600" r="250" fill="#c5cae9" opacity="0.25"/><circle cx="960" cy="200" r="150" fill="#b2dfdb" opacity="0.2"/>
</svg>`
}

func lightPeachSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%">
<stop offset="0%" stop-color="#fff5eb"/><stop offset="50%" stop-color="#ffe4d1"/><stop offset="100%" stop-color="#ffd6c2"/>
</linearGradient></defs>
<rect width="1920" height="1080" fill="url(#g)"/>
<circle cx="1600" cy="250" r="350" fill="#ffccbc" opacity="0.3"/><circle cx="300" cy="800" r="200" fill="#fff9c4" opacity="0.3"/>
</svg>`
}

func developerCodeSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<rect width="1920" height="1080" fill="#0d1117"/>
<g opacity="0.12" font-family="monospace" font-size="14" fill="#7ee787">
<text>x1 = 42</text><text y="24">y2 = x1 + 10</text><text y="48">result = compute(x1, y2)</text><text y="72">if result &gt; 0:</text><text y="96" x="24">    return True</text><text y="120" x="24">else:</text><text y="144" x="24">    return False</text><text y="168">def main():</text><text y="192" x="24">    pass</text><text y="216">for i in range(100):</text><text y="240" x="24">    print(i)</text><text y="264">class Processor:</text><text y="288" x="24">    def __init__(self):</text><text y="312" x="48">        self.data = []</text><text y="336">    async def run(self):</text><text y="360" x="24">        await self.process()</text><text y="384">async def process_all():</text><text y="408" x="24">    await asyncio.gather(*tasks)</text><text y="432">import asyncio</text><text y="456">from typing import Optional, List</text><text y="480">@dataclass</text><text y="504" x="24">class Config:</text><text y="528" x="48">    host: str = &quot;localhost&quot;</text><text y="552" x="48">    port: int = 8080</text><text y="576">    debug: bool = False</text><text y="600">if __name__ == &quot;__main__&quot;:</text><text y="624" x="24">    main()</text></g>
</svg>`
}

func developerTerminalSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<rect width="1920" height="1080" fill="#000000"/>
<g font-family="monospace" font-size="14" fill="#00ff41" opacity="0.15">
<text x="20" y="30">user@ocode:~$ npm run dev</text><text y="50">[OK] compiled in 1.23s</text><text y="70">VITE v5.4.0  ready in 320ms</text><text y="90">&#3542;  Local:   http://localhost:5173/</text><text y="110">&#3542;  Network: use --host to expose</text><text y="150">user@ocode:~$ git status</text><text y="170">On branch main</text><text y="190">Your branch is up to date.</text><text y="210">nothing to commit, working tree clean</text><text y="250">user@ocode:~$ go build ./...</text><text y="270">user@ocode:~$ go test ./...</text><text y="290">ok  github.com/u007/ocode  45.23s</text><text y="310">user@ocode:~$ docker compose up -d</text><text y="330">[OK] Container ocode-server-1  Running</text><text y="350">[OK] Container ocode-db-1     Running</text><text y="400">user@ocode:~$ kubectl get pods</text><text y="420">NAME              READY   STATUS</text><text y="440">ocode-server-7d8f   1/1     Running</text><text y="460">ocode-worker-3k2x  1/1     Running</text></g>
</svg>`
}

func cuteDotsSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<rect width="1920" height="1080" fill="#fff0f5"/>
<g opacity="0.4">
<circle cx="100" cy="100" r="12" fill="#ffb6c1"/><circle cx="300" cy="200" r="18" fill="#dda0dd"/><circle cx="500" cy="150" r="10" fill="#ffc0cb"/><circle cx="700" cy="300" r="15" fill="#e6b0aa"/>
<circle cx="900" cy="100" r="20" fill="#ff99cc"/><circle cx="1100" cy="250" r="12" fill="#da70d6"/><circle cx="1300" cy="150" r="16" fill="#ffb6c1"/><circle cx="1500" cy="350" r="14" fill="#d8bfd8"/>
<circle cx="1700" cy="200" r="18" fill="#ff69b4"/><circle cx="200" cy="400" r="10" fill="#ffc0cb"/><circle cx="400" cy="500" r="16" fill="#dda0dd"/><circle cx="600" cy="350" r="12" fill="#e6b0aa"/>
<circle cx="800" cy="550" r="20" fill="#ff99cc"/><circle cx="1000" cy="450" r="14" fill="#da70d6"/><circle cx="1200" cy="600" r="18" fill="#ffb6c1"/><circle cx="1400" cy="500" r="10" fill="#d8bfd8"/>
<circle cx="1600" cy="650" r="16" fill="#ff69b4"/><circle cx="1800" cy="400" r="12" fill="#dda0dd"/><circle cx="150" cy="700" r="14" fill="#ffc0cb"/><circle cx="350" cy="800" r="18" fill="#e6b0aa"/>
<circle cx="550" cy="750" r="10" fill="#ff99cc"/><circle cx="750" cy="900" r="16" fill="#da70d6"/><circle cx="950" cy="800" r="12" fill="#ffb6c1"/><circle cx="1150" cy="950" r="20" fill="#d8bfd8"/>
<circle cx="1350" cy="850" r="14" fill="#ff69b4"/><circle cx="1550" cy="700" r="18" fill="#dda0dd"/><circle cx="1750" cy="850" r="10" fill="#ffc0cb"/><circle cx="600" cy="600" r="25" fill="#ffb6c1" opacity="0.2"/>
<circle cx="1200" cy="400" r="30" fill="#dda0dd" opacity="0.15"/>
</g>
</svg>`
}

func cuteHeartsSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<rect width="1920" height="1080" fill="#fff0f5"/>
<g opacity="0.35">
<path d="M50,80 C50,60 70,50 80,65 C90,50 110,60 110,80 C110,110 80,130 80,130 C80,130 50,110 50,80Z" fill="#ff69b4"/>
<path d="M300,200 C300,180 320,170 330,185 C340,170 360,180 360,200 C360,230 330,250 330,250 C330,250 300,230 300,200Z" fill="#ffb6c1"/>
<path d="M600,100 C600,80 620,70 630,85 C640,70 660,80 660,100 C660,130 630,150 630,150 C630,150 600,130 600,100Z" fill="#dda0dd"/>
<path d="M900,300 C900,280 920,270 930,285 C940,270 960,280 960,300 C960,330 930,350 930,350 C930,350 900,330 900,300Z" fill="#ff69b4"/>
<path d="M1200,150 C1200,130 1220,120 1230,135 C1240,120 1260,130 1260,150 C1260,180 1230,200 1230,200 C1230,200 1200,180 1200,150Z" fill="#ffb6c1"/>
<path d="M1500,250 C1500,230 1520,220 1530,235 C1540,220 1560,230 1560,250 C1560,280 1530,300 1530,300 C1530,300 1500,280 1500,250Z" fill="#dda0dd"/>
<path d="M200,500 C200,480 220,470 230,485 C240,470 260,480 260,500 C260,530 230,550 230,550 C230,550 200,530 200,500Z" fill="#ff99cc"/>
<path d="M700,600 C700,580 720,570 730,585 C740,570 760,580 760,600 C760,630 730,650 730,650 C730,650 700,630 700,600Z" fill="#ffb6c1"/>
<path d="M1100,700 C1100,680 1120,670 1130,685 C1140,670 1160,680 1160,700 C1160,730 1130,750 1130,750 C1130,750 1100,730 1100,700Z" fill="#ff69b4"/>
<path d="M1400,550 C1400,530 1420,520 1430,535 C1440,520 1460,530 1460,550 C1460,580 1430,600 1430,600 C1430,600 1400,580 1400,550Z" fill="#dda0dd"/>
<path d="M1700,450 C1700,430 1720,420 1730,435 C1740,420 1760,430 1760,450 C1760,480 1730,500 1730,500 C1730,500 1700,480 1700,450Z" fill="#ff99cc"/>
</g>
</svg>`
}

func cuteStarsSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1920" height="1080" viewBox="0 0 1920 1080">
<rect width="1920" height="1080" fill="#1a0a2e"/>
<g opacity="0.6">
<path d="M100,80 L104,92 L116,92 L106,100 L110,112 L100,104 L90,112 L94,100 L84,92 L96,92Z" fill="#fff"/>
<path d="M300,200 L302,206 L308,206 L303,210 L305,216 L300,212 L295,216 L297,210 L292,206 L298,206Z" fill="#e0e0e0"/>
<path d="M500,150 L503,160 L512,160 L505,166 L508,176 L500,170 L492,176 L495,166 L488,160 L497,160Z" fill="#fff"/>
<path d="M700,300 L702,306 L708,306 L703,310 L705,316 L700,312 L695,316 L697,310 L692,306 L698,306Z" fill="#f0f0f0"/>
<path d="M900,100 L904,112 L916,112 L906,120 L910,132 L900,124 L890,132 L894,120 L884,112 L896,112Z" fill="#fff"/>
<path d="M1100,250 L1103,260 L1112,260 L1105,266 L1108,276 L1100,270 L1092,276 L1095,266 L1088,260 L1097,260Z" fill="#e8d5f5"/>
<path d="M1300,400 L1302,406 L1308,406 L1303,410 L1305,416 L1300,412 L1295,416 L1297,410 L1292,406 L1298,406Z" fill="#fff"/>
<path d="M1500,550 L1504,568 L1522,568 L1508,580 L1514,598 L1500,586 L1486,598 L1492,580 L1478,568 L1496,568Z" fill="#fff"/>
<path d="M1700,200 L1702,206 L1708,206 L1703,210 L1705,216 L1700,212 L1695,216 L1697,210 L1692,206 L1698,206Z" fill="#e0e0e0"/>
<path d="M200,600 L203,610 L212,610 L205,616 L208,626 L200,620 L192,626 L195,616 L188,610 L197,610Z" fill="#fff"/>
<path d="M600,750 L604,668 L622,668 L610,656 L616,638 L600,650 L584,638 L590,656 L578,668 L596,668Z" fill="#f0e0ff"/>
<path d="M1000,900 L1003,910 L1012,910 L1005,916 L1008,926 L1000,920 L992,926 L995,916 L988,910 L997,910Z" fill="#fff"/>
<path d="M1400,800 L1402,806 L1408,806 L1403,810 L1405,816 L1400,812 L1395,816 L1397,810 L1392,806 L1398,806Z" fill="#e0e0e0"/>
<path d="M1800,700 L1804,716 L1822,716 L1808,728 L1814,746 L1800,734 L1786,746 L1792,728 L1778,716 L1796,716Z" fill="#fff"/>
</g>
</svg>`
}
