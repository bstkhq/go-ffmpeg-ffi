// Package mobile is a downstream Android integration fixture. It deliberately
// lives in its own Go module so Ebitengine never becomes a dependency of
// go-ffmpeg-ffi itself.
package mobile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/bstkhq/go-ffmpeg-ffi"
	"github.com/bstkhq/go-ffmpeg-ffi-android-ebiten-test/internal/rgba"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/mobile"
)

const (
	logicalWidth  = 640
	logicalHeight = 360
	audioRate     = 48_000
)

type probeGame struct {
	mu              sync.RWMutex
	status          string
	success         bool
	framePixels     []byte
	frameWidth      int
	frameHeight     int
	frameGeneration uint64
	runCancel       context.CancelFunc
	runDone         chan struct{}

	// The following fields are touched only by Ebitengine's render goroutine.
	videoImage          *ebiten.Image
	presentedGeneration uint64

	// Retain the audio objects after the decoding goroutine returns.
	audioContext *audio.Context
	audioPlayer  *audio.Player
}

func newProbeGame() *probeGame {
	return &probeGame{status: "Waiting for the Android host..."}
}

func (g *probeGame) start(mediaPath string) {
	g.mu.Lock()
	if g.runDone != nil {
		g.mu.Unlock()
		log.Print("Android probe start ignored: an activity run is already registered")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	g.runCancel = cancel
	g.runDone = done
	g.status = "Ebitengine is running. Loading FFmpeg..."
	g.success = false
	g.mu.Unlock()

	go func() {
		defer close(done)
		g.run(ctx, mediaPath)
	}()
}

func (g *probeGame) run(ctx context.Context, mediaPath string) {
	err := ffmpeg.Init()
	diagnostic := ffmpeg.Diagnose()
	if err != nil {
		status := fmt.Sprintf(
			"Ebitengine + go-ffmpeg-ffi Android probe\n\n"+
				"Platform: %s/%s\n"+
				"FFmpeg load: FAILED\n%s\n\n"+
				"This is expected until the FFmpeg .so files are packaged.\n\n%s",
			runtime.GOOS, runtime.GOARCH, err, diagnostic.String(),
		)
		log.Print(status)
		g.setStatus(status, false)
		return
	}

	loadStatus := fmt.Sprintf(
		"Ebitengine + go-ffmpeg-ffi Android probe\n\n"+
			"Platform: %s/%s\n"+
			"FFmpeg load: OK\n\n%s",
		runtime.GOOS, runtime.GOARCH, diagnostic.String(),
	)
	log.Print(loadStatus)
	g.setStatus(loadStatus+"\n\nDecoding H.264 fixture...", false)

	frames, width, height, decodeErr := g.decodeVideo(ctx, mediaPath)
	if errors.Is(decodeErr, context.Canceled) {
		log.Print("Android lifecycle canceled the video probe")
		return
	}
	if decodeErr != nil {
		status := fmt.Sprintf(
			"Ebitengine + go-ffmpeg-ffi Android probe\n\n"+
				"Platform: %s/%s\n"+
				"FFmpeg load: OK\n"+
				"H.264 decode/scale: FAILED\n%s",
			runtime.GOOS, runtime.GOARCH, decodeErr,
		)
		log.Print(status)
		g.setStatus(status, false)
		return
	}

	g.setStatus(fmt.Sprintf(
		"Ebitengine + go-ffmpeg-ffi Android probe\n"+
			"FFmpeg load: OK | H.264 decode: OK | seek/EOF/cancel: OK\n"+
			"Video: %dx%d, %d frames -> RGBA -> Ebitengine\n"+
			"Decoding AAC fixture...",
		width, height, frames,
	), false)

	audioFrames, audioSamples, audioErr := g.decodeAndPlayAudio(ctx, mediaPath)
	if errors.Is(audioErr, context.Canceled) {
		log.Print("Android lifecycle canceled the audio probe")
		return
	}
	if audioErr != nil {
		status := fmt.Sprintf(
			"Ebitengine + go-ffmpeg-ffi Android probe\n"+
				"FFmpeg load: OK | H.264 decode: OK | seek/EOF/cancel: OK\n"+
				"Video: %dx%d, %d frames -> RGBA -> Ebitengine\n"+
				"AAC decode/resample/playback: FAILED\n%s",
			width, height, frames, audioErr,
		)
		log.Print(status)
		g.setStatus(status, false)
		return
	}

	status := fmt.Sprintf(
		"Ebitengine + go-ffmpeg-ffi Android probe\n"+
			"FFmpeg/H.264/AAC: OK | seek/EOF/cancel: OK\n"+
			"Video: %dx%d, %d frames -> RGBA -> Ebitengine\n"+
			"Audio: %d frames, %d samples -> S16 stereo 48 kHz -> Ebitengine",
		width, height, frames, audioFrames, audioSamples,
	)
	log.Print(status)
	g.setStatus(status, true)
}

func (g *probeGame) decodeAndPlayAudio(ctx context.Context, mediaPath string) (frames, samples int, err error) {
	decoder, err := ffmpeg.NewDecoder(mediaPath, &ffmpeg.DecoderOptions{
		Streams: []ffmpeg.MediaType{ffmpeg.MediaTypeAudio},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("open media fixture: %w", err)
	}
	defer func() {
		if closeErr := decoder.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close audio decoder: %w", closeErr)
		}
	}()

	stream := decoder.AudioStream()
	if stream == nil {
		return 0, 0, fmt.Errorf("media fixture has no audio stream")
	}
	if stream.CodecID != ffmpeg.CodecIDAAC {
		return 0, 0, fmt.Errorf("audio codec is %s, want AAC", stream.CodecName)
	}

	var pcm bytes.Buffer
	var resampler *ffmpeg.Resampler
	defer func() {
		if resampler != nil {
			if closeErr := resampler.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("close audio resampler: %w", closeErr)
			}
		}
	}()

	appendPCM := func(frame ffmpeg.Frame) error {
		if frame.IsNil() {
			return fmt.Errorf("resampler returned a nil frame")
		}
		data := frame.Data(0)
		if frame.NbSamples() > 0 && len(data) == 0 {
			return fmt.Errorf("resampler returned %d samples without packed PCM data", frame.NbSamples())
		}
		samples += frame.NbSamples()
		_, writeErr := pcm.Write(data)
		return writeErr
	}

	for {
		frame, decodeErr := decoder.DecodeAudioContext(ctx)
		if decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			return frames, samples, fmt.Errorf("decode audio frame %d: %w", frames, decodeErr)
		}
		if frame.IsNil() {
			break
		}

		if resampler == nil {
			sampleRate := frame.SampleRate()
			if sampleRate == 0 {
				sampleRate = stream.SampleRate
			}
			resampler, err = ffmpeg.NewResampler(
				ffmpeg.AudioFormat{
					SampleRate:   sampleRate,
					Channels:     stream.Channels,
					SampleFormat: frame.SampleFormat(),
				},
				ffmpeg.AudioFormat{
					SampleRate:    audioRate,
					Channels:      2,
					ChannelLayout: ffmpeg.ChannelLayoutStereo,
					SampleFormat:  ffmpeg.SampleFormatS16,
				},
			)
			if err != nil {
				return frames, samples, fmt.Errorf("create AAC-to-PCM resampler: %w", err)
			}
		}

		resampled, resampleErr := resampler.Resample(frame)
		if resampleErr != nil {
			return frames, samples, fmt.Errorf("resample audio frame %d: %w", frames, resampleErr)
		}
		if !resampled.IsNil() {
			if appendErr := appendPCM(resampled); appendErr != nil {
				_ = ffmpeg.FrameFree(&resampled)
				return frames, samples, appendErr
			}
			_ = ffmpeg.FrameFree(&resampled)
		}
		frames++
	}

	if resampler == nil || frames == 0 {
		return 0, 0, fmt.Errorf("decoder produced no audio frames")
	}
	flushed, flushErr := resampler.Flush()
	if flushErr != nil {
		return frames, samples, fmt.Errorf("flush audio resampler: %w", flushErr)
	}
	if !flushed.IsNil() {
		if appendErr := appendPCM(flushed); appendErr != nil {
			_ = ffmpeg.FrameFree(&flushed)
			return frames, samples, appendErr
		}
		_ = ffmpeg.FrameFree(&flushed)
	}
	if pcm.Len() == 0 {
		return frames, samples, fmt.Errorf("resampler produced no PCM data")
	}

	if err := ctx.Err(); err != nil {
		return frames, samples, err
	}
	audioContext := audio.CurrentContext()
	if audioContext == nil {
		audioContext = audio.NewContext(audioRate)
	}
	if audioContext.SampleRate() != audioRate {
		return frames, samples, fmt.Errorf("Ebitengine audio context rate is %d, want %d", audioContext.SampleRate(), audioRate)
	}
	audioPlayer, err := audioContext.NewPlayer(bytes.NewReader(pcm.Bytes()))
	if err != nil {
		return frames, samples, fmt.Errorf("create Ebitengine audio player: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = audioPlayer.Close()
		return frames, samples, err
	}
	g.mu.Lock()
	g.audioContext = audioContext
	g.audioPlayer = audioPlayer
	audioPlayer.Play()
	g.mu.Unlock()
	log.Printf(
		"AAC audio accepted by Ebitengine: frames=%d samples=%d pcm_bytes=%d rate=%d channels=2 format=s16",
		frames, samples, pcm.Len(), audioRate,
	)
	return frames, samples, nil
}

func (g *probeGame) decodeVideo(ctx context.Context, mediaPath string) (frames, width, height int, err error) {
	if mediaPath == "" {
		return 0, 0, 0, fmt.Errorf("media fixture path was not provided by Android")
	}

	decoder, err := ffmpeg.NewDecoder(mediaPath, &ffmpeg.DecoderOptions{
		Streams: []ffmpeg.MediaType{ffmpeg.MediaTypeVideo},
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open media fixture: %w", err)
	}
	defer func() {
		if closeErr := decoder.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close video decoder: %w", closeErr)
		}
	}()

	stream := decoder.VideoStream()
	if stream == nil {
		return 0, 0, 0, fmt.Errorf("media fixture has no video stream")
	}
	if stream.CodecID != ffmpeg.CodecIDH264 {
		return 0, 0, 0, fmt.Errorf("video codec is %s, want H.264", stream.CodecName)
	}

	var scaler *ffmpeg.Scaler
	defer func() {
		if scaler != nil {
			if closeErr := scaler.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("close RGBA scaler: %w", closeErr)
			}
		}
	}()

	for {
		frame, decodeErr := decoder.DecodeVideoContext(ctx)
		if decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			return frames, width, height, fmt.Errorf("decode frame %d: %w", frames, decodeErr)
		}
		if frame.IsNil() {
			break
		}

		info := ffmpeg.GetFrameInfo(frame)
		if scaler == nil {
			width, height = info.Width, info.Height
			scaler, err = ffmpeg.NewScaler(
				width, height, ffmpeg.PixelFormat(info.Format),
				width, height, ffmpeg.PixelFormatRGBA, ffmpeg.ScaleBilinear,
			)
			if err != nil {
				return frames, width, height, fmt.Errorf("create RGBA scaler: %w", err)
			}
		}

		rgba, scaleErr := scaler.Scale(frame)
		if scaleErr != nil {
			return frames, width, height, fmt.Errorf("scale frame %d: %w", frames, scaleErr)
		}
		if publishErr := g.publishFrame(rgba, width, height); publishErr != nil {
			return frames, width, height, fmt.Errorf("publish frame %d: %w", frames, publishErr)
		}
		frames++
	}

	if frames == 0 {
		return 0, width, height, fmt.Errorf("decoder produced no video frames")
	}

	// EOF must remain stable until a seek resets the decoder state.
	eofFrame, eofErr := decoder.DecodeVideoContext(ctx)
	if !errors.Is(eofErr, io.EOF) {
		return frames, width, height, fmt.Errorf("repeat video EOF = %v, want io.EOF", eofErr)
	}
	if !eofFrame.IsNil() {
		return frames, width, height, fmt.Errorf("decoder produced a frame after EOF")
	}

	// Seek to the preceding keyframe and decode forward to one second. This is
	// frame-accurate while keeping every blocking operation cancellable.
	if seekErr := decoder.SeekContext(ctx, time.Second); seekErr != nil {
		return frames, width, height, fmt.Errorf("seek video to one second: %w", seekErr)
	}
	timeBase := stream.TimeBase
	if timeBase.Num <= 0 || timeBase.Den <= 0 {
		return frames, width, height, fmt.Errorf("video stream has invalid time base %d/%d", timeBase.Num, timeBase.Den)
	}
	targetPTS := int64(timeBase.Den) / int64(timeBase.Num)
	var seekFrame ffmpeg.Frame
	for {
		seekFrame, err = decoder.DecodeVideoContext(ctx)
		if err != nil {
			return frames, width, height, fmt.Errorf("decode frame after seek: %w", err)
		}
		if seekFrame.IsNil() {
			return frames, width, height, fmt.Errorf("decoder reached EOF before seek target")
		}
		if ffmpeg.GetFrameInfo(seekFrame).PTS >= targetPTS {
			break
		}
	}
	seekRGBA, scaleErr := scaler.Scale(seekFrame)
	if scaleErr != nil {
		return frames, width, height, fmt.Errorf("scale seek frame: %w", scaleErr)
	}
	if publishErr := g.publishFrame(seekRGBA, width, height); publishErr != nil {
		return frames, width, height, fmt.Errorf("publish seek frame: %w", publishErr)
	}
	seekPTS := ffmpeg.GetFrameInfo(seekFrame).PTS

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, cancelErr := decoder.DecodeVideoContext(cancelCtx); !errors.Is(cancelErr, context.Canceled) {
		return frames, width, height, fmt.Errorf("canceled decode returned %v, want context canceled", cancelErr)
	}
	resumeFrame, resumeErr := decoder.DecodeVideoContext(ctx)
	if resumeErr != nil {
		return frames, width, height, fmt.Errorf("resume decoder after cancellation: %w", resumeErr)
	}
	if resumeFrame.IsNil() {
		return frames, width, height, fmt.Errorf("decoder did not resume after cancellation")
	}
	log.Printf("H.264 seek/EOF/cancellation checks passed: frames=%d seek_pts=%d", frames, seekPTS)
	return frames, width, height, nil
}

func (g *probeGame) publishFrame(frame ffmpeg.Frame, width, height int) error {
	if frame.IsNil() {
		return fmt.Errorf("scaled frame is nil")
	}
	data := frame.Data(0)
	stride := frame.Linesize(0)
	pixels, err := rgba.Pack(data, stride, width, height)
	if err != nil {
		return err
	}

	g.mu.Lock()
	g.framePixels = pixels
	g.frameWidth = width
	g.frameHeight = height
	g.frameGeneration++
	g.mu.Unlock()
	return nil
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
	frameWidth := g.frameWidth
	frameHeight := g.frameHeight
	frameGeneration := g.frameGeneration
	var pixels []byte
	if frameGeneration != g.presentedGeneration {
		pixels = append(pixels, g.framePixels...)
	}
	g.mu.RUnlock()
	if frameGeneration != g.presentedGeneration && len(pixels) != 0 {
		if g.videoImage == nil || g.videoImage.Bounds().Dx() != frameWidth || g.videoImage.Bounds().Dy() != frameHeight {
			g.videoImage = ebiten.NewImage(frameWidth, frameHeight)
		}
		g.videoImage.WritePixels(pixels)
		g.presentedGeneration = frameGeneration
	}

	background := color.RGBA{R: 34, G: 39, B: 46, A: 255}
	accent := color.RGBA{R: 214, G: 84, B: 72, A: 255}
	if success {
		accent = color.RGBA{R: 62, G: 166, B: 96, A: 255}
	}
	screen.Fill(background)
	ebitenutil.DrawRect(screen, 0, 0, logicalWidth, 12, accent)

	if g.videoImage != nil {
		const top = 104
		availableHeight := float64(logicalHeight - top)
		scale := min(float64(logicalWidth)/float64(frameWidth), availableHeight/float64(frameHeight))
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(scale, scale)
		op.GeoM.Translate(
			(float64(logicalWidth)-float64(frameWidth)*scale)/2,
			float64(top),
		)
		screen.DrawImage(g.videoImage, op)
	}

	ebitenutil.DebugPrintAt(screen, strings.TrimSpace(status), 24, 26)
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
func SetAndroidID(_ int64) {}

// SetMediaPath receives the private path prepared from the APK's test asset.
func SetMediaPath(path string) {
	game.start(path)
}

// Shutdown is called from MainActivity.onDestroy. It cancels native work,
// waits for FFmpeg resources to close, and releases the Ebitengine player.
func Shutdown() {
	game.shutdown()
}

// SetTimezone is an optional hook detected by apk-ebiten-builder.
func SetTimezone(_ string) {}

func (g *probeGame) shutdown() {
	g.mu.Lock()
	cancel := g.runCancel
	done := g.runDone
	g.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			log.Print("Android lifecycle shutdown: FAILED: timed out waiting for FFmpeg resources")
			return
		}
	}

	g.mu.Lock()
	player := g.audioPlayer
	g.audioPlayer = nil
	g.runCancel = nil
	g.runDone = nil
	g.mu.Unlock()

	if player != nil {
		if err := player.Close(); err != nil {
			log.Printf("Android lifecycle shutdown: FAILED: close audio player: %v", err)
			return
		}
	}
	log.Print("Android lifecycle shutdown: OK")
}
