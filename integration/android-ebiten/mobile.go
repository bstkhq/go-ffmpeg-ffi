// Package mobile is a downstream Android integration fixture. It deliberately
// lives in its own Go module so Ebitengine never becomes a dependency of
// go-ffmpeg-ffi itself.
package mobile

import (
	"fmt"
	"image/color"
	"runtime"
	"strings"
	"sync"

	ffgo "github.com/bstkhq/go-ffmpeg-ffi"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/mobile"
)

const (
	logicalWidth  = 640
	logicalHeight = 360
)

type probeGame struct {
	mu      sync.RWMutex
	status  string
	success bool
	started sync.Once
}

func newProbeGame() *probeGame {
	return &probeGame{status: "Waiting for the Android host..."}
}

func (g *probeGame) start() {
	g.started.Do(func() {
		g.setStatus("Ebitengine is running. Loading FFmpeg...", false)
		go func() {
			err := ffgo.Init()
			diagnostic := ffgo.Diagnose()
			if err != nil {
				g.setStatus(fmt.Sprintf(
					"Ebitengine + go-ffmpeg-ffi Android probe\n\n"+
						"Platform: %s/%s\n"+
						"FFmpeg load: FAILED\n%s\n\n"+
						"This is expected until the FFmpeg .so files are packaged.\n\n%s",
					runtime.GOOS, runtime.GOARCH, err, diagnostic.String(),
				), false)
				return
			}

			g.setStatus(fmt.Sprintf(
				"Ebitengine + go-ffmpeg-ffi Android probe\n\n"+
					"Platform: %s/%s\n"+
					"FFmpeg load: OK\n\n%s",
				runtime.GOOS, runtime.GOARCH, diagnostic.String(),
			), true)
		}()
	})
}

func (g *probeGame) setStatus(status string, success bool) {
	g.mu.Lock()
	g.status = status
	g.success = success
	g.mu.Unlock()
}

func (g *probeGame) Update() error { return nil }

func (g *probeGame) Draw(screen *ebiten.Image) {
	g.mu.RLock()
	status := g.status
	success := g.success
	g.mu.RUnlock()

	background := color.RGBA{R: 34, G: 39, B: 46, A: 255}
	accent := color.RGBA{R: 214, G: 84, B: 72, A: 255}
	if success {
		accent = color.RGBA{R: 62, G: 166, B: 96, A: 255}
	}
	screen.Fill(background)
	ebitenutil.DrawRect(screen, 0, 0, logicalWidth, 12, accent)

	ebitenutil.DebugPrintAt(screen, strings.TrimSpace(status), 24, 32)
}

func (g *probeGame) Layout(_, _ int) (int, int) {
	return logicalWidth, logicalHeight
}

var game = newProbeGame()

func init() {
	mobile.SetGame(game)
}

// IMEBridge matches the Java bridge supplied by apk-ebiten-builder.
type IMEBridge interface {
	Show(inputType, imeOptions int32)
	Hide()
	Composing() string
}

// RegisterIMEBridge is exported for apk-ebiten-builder's MainActivity.
func RegisterIMEBridge(IMEBridge) {}

// SetAndroidID is called by apk-ebiten-builder after the Android activity is
// created. The identifier is intentionally unused by this diagnostic fixture.
func SetAndroidID(_ int64) {
	game.start()
}

// SetTimezone is an optional hook detected by apk-ebiten-builder.
func SetTimezone(_ string) {}
