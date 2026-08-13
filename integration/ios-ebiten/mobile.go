// Package mobile is a downstream iOS integration fixture. It deliberately
// lives in its own Go module so Ebitengine never becomes a dependency of
// go-ffmpeg-ffi itself.
package mobile

import (
	"fmt"
	"image/color"
	"runtime"
	"sync"

	"github.com/bstkhq/go-ffmpeg-ffi"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/mobile"
)

const (
	logicalWidth  = 640
	logicalHeight = 360
)

type probeGame struct {
	once   sync.Once
	mu     sync.RWMutex
	status string
}

func (g *probeGame) Update() error {
	g.once.Do(func() {
		go g.probe()
	})
	return nil
}

func (g *probeGame) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{R: 18, G: 24, B: 31, A: 255})
	g.mu.RLock()
	status := g.status
	g.mu.RUnlock()
	ebitenutil.DebugPrint(screen, status)
}

func (*probeGame) Layout(_, _ int) (int, int) {
	return logicalWidth, logicalHeight
}

func (g *probeGame) probe() {
	err := ffgo.Init()
	diagnostic := ffgo.Diagnose()
	result := fmt.Sprintf(
		"Ebitengine + go-ffmpeg-ffi iOS probe\n\nPlatform: %s/%s\n",
		runtime.GOOS,
		runtime.GOARCH,
	)
	if err != nil {
		result += fmt.Sprintf(
			"FFmpeg load: FAILED\n%s\n\nEmbed signed FFmpeg frameworks or link FFmpeg into the app image.\n\n%s",
			err,
			diagnostic.String(),
		)
	} else {
		result += "FFmpeg load: OK\n\n" + diagnostic.String()
	}
	g.mu.Lock()
	g.status = result
	g.mu.Unlock()
}

func init() {
	mobile.SetGame(&probeGame{status: "Ebitengine is running. Loading FFmpeg..."})
}

// Dummy forces gomobile to compile this package and expose a binding.
func Dummy() {}
