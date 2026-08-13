//go:build amd64 || arm64

package ffmpeg

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/handles"
)

func TestDecoderLifecycle(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	runDecoderLifecycleTest(t, 8, false)
}

func TestDecoderLifecycleStress(t *testing.T) {
	if os.Getenv("FFMPEG_STRESS") == "" {
		t.Skip("set FFMPEG_STRESS=1 to run prolonged memory assertions")
	}
	if !requireFFmpeg(t) {
		return
	}

	iterations := 100
	if value := os.Getenv("FFMPEG_STRESS_ITERATIONS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			t.Fatalf("FFMPEG_STRESS_ITERATIONS must be a positive integer, got %q", value)
		}
		iterations = parsed
	}
	runDecoderLifecycleTest(t, iterations, true)
}

func runDecoderLifecycleTest(t *testing.T, iterations int, assertMemory bool) {
	t.Helper()
	input := createTestVideo(t)
	runtime.GC()
	if assertMemory {
		debug.FreeOSMemory()
	}
	baselineHandles := handles.Count()
	baselineFDs := openFileDescriptors()
	baselineRSS := int64(-1)
	var baselineMemory runtime.MemStats
	if assertMemory {
		baselineRSS = residentMemoryBytes()
		runtime.ReadMemStats(&baselineMemory)
	}

	const workers = 4
	jobs := make(chan int)
	errorsFound := make(chan error, iterations)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for iteration := range jobs {
				if err := exerciseDecoderLifecycle(input); err != nil {
					errorsFound <- fmt.Errorf("iteration %d: %w", iteration, err)
				}
			}
		}()
	}
	for iteration := 0; iteration < iterations; iteration++ {
		jobs <- iteration
	}
	close(jobs)
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	runtime.GC()
	if assertMemory {
		debug.FreeOSMemory()
	}
	var finalMemory runtime.MemStats
	if assertMemory {
		runtime.ReadMemStats(&finalMemory)
	}
	if got := handles.Count(); got != baselineHandles {
		t.Fatalf("registered handles = %d, want baseline %d", got, baselineHandles)
	}
	if finalFDs := openFileDescriptors(); baselineFDs >= 0 && finalFDs > baselineFDs+2 {
		t.Fatalf("open file descriptors grew from %d to %d", baselineFDs, finalFDs)
	}
	if assertMemory && finalMemory.HeapAlloc > baselineMemory.HeapAlloc+(32<<20) {
		t.Fatalf("Go heap grew from %d to %d bytes", baselineMemory.HeapAlloc, finalMemory.HeapAlloc)
	}
	if assertMemory {
		if finalRSS := residentMemoryBytes(); baselineRSS >= 0 && finalRSS > baselineRSS+(64<<20) {
			t.Fatalf("resident memory grew from %d to %d bytes", baselineRSS, finalRSS)
		}
	}
}

func exerciseDecoderLifecycle(input string) error {
	decoder, err := NewDecoder(input, nil)
	if err != nil {
		return err
	}
	if !decoder.HasVideo() || !decoder.HasAudio() {
		_ = decoder.Close()
		return fmt.Errorf("audiovisual fixture is missing a selected stream")
	}

	seen := map[MediaType]int{}
	for reads := 0; reads < 64 && (seen[MediaTypeVideo] < 2 || seen[MediaTypeAudio] < 2); reads++ {
		frame, readErr := decoder.ReadFrame()
		if readErr != nil {
			_ = decoder.Close()
			return readErr
		}
		if frame == nil {
			break
		}
		seen[frame.MediaType()]++
	}
	if seen[MediaTypeVideo] < 2 || seen[MediaTypeAudio] < 2 {
		_ = decoder.Close()
		return fmt.Errorf("decoded video=%d audio=%d frames", seen[MediaTypeVideo], seen[MediaTypeAudio])
	}
	if err := decoder.Seek(250 * time.Millisecond); err != nil {
		_ = decoder.Close()
		return err
	}
	if _, err := decoder.DecodeVideo(); err != nil {
		_ = decoder.Close()
		return err
	}
	return decoder.Close()
}

func openFileDescriptors() int {
	if runtime.GOOS != "linux" {
		return -1
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

func residentMemoryBytes() int64 {
	if runtime.GOOS != "linux" {
		return -1
	}
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return -1
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return -1
	}
	residentPages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return -1
	}
	return residentPages * int64(os.Getpagesize())
}
