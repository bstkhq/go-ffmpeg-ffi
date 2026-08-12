//go:build !ios && (amd64 || arm64)

package ffgo

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestDecoderNativeAccessorsConcurrentClose(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t))
	if err != nil {
		t.Fatal(err)
	}

	const readers = 8
	start := make(chan struct{})
	stop := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(readers)
	done.Add(readers)
	for range readers {
		go func() {
			defer done.Done()
			<-start
			first := true
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = decoder.NumStreams()
				_ = decoder.DurationMicroseconds()
				_ = decoder.BitRate()
				_ = decoder.ProbeScore()
				_ = decoder.Programs()
				_ = decoder.DataStreams()
				_ = decoder.HasSubtitle()
				_ = decoder.SubtitleStream()
				_ = decoder.TotalFrames()
				if first {
					first = false
					ready.Done()
				}
			}
		}()
	}

	close(start)
	ready.Wait()
	runtime.Gosched()
	if err := decoder.Close(); err != nil {
		t.Fatal(err)
	}
	close(stop)
	done.Wait()

	if got := decoder.NumStreams(); got != 0 {
		t.Errorf("NumStreams after Close = %d, want 0", got)
	}
	if got := decoder.DurationMicroseconds(); got != 0 {
		t.Errorf("DurationMicroseconds after Close = %d, want 0", got)
	}
	if got := decoder.BitRate(); got != 0 {
		t.Errorf("BitRate after Close = %d, want 0", got)
	}
	if got := decoder.ProbeScore(); got != 0 {
		t.Errorf("ProbeScore after Close = %d, want 0", got)
	}
}

func TestDecoderNativeAccessorsUseOperationLock(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	accessors := []struct {
		name string
		call func()
	}{
		{name: "NumStreams", call: func() { _ = decoder.NumStreams() }},
		{name: "DurationMicroseconds", call: func() { _ = decoder.DurationMicroseconds() }},
		{name: "BitRate", call: func() { _ = decoder.BitRate() }},
		{name: "ProbeScore", call: func() { _ = decoder.ProbeScore() }},
		{name: "Programs", call: func() { _ = decoder.Programs() }},
		{name: "DataStreams", call: func() { _ = decoder.DataStreams() }},
		{name: "HasSubtitle", call: func() { _ = decoder.HasSubtitle() }},
		{name: "SubtitleStream", call: func() { _ = decoder.SubtitleStream() }},
		{name: "TotalFrames", call: func() { _ = decoder.TotalFrames() }},
	}

	decoder.mu.Lock()
	started := make(chan struct{}, len(accessors))
	finished := make(chan string, len(accessors))
	for _, accessor := range accessors {
		go func() {
			started <- struct{}{}
			accessor.call()
			finished <- accessor.name
		}()
	}
	for range accessors {
		<-started
	}
	select {
	case name := <-finished:
		decoder.mu.Unlock()
		t.Fatalf("%s returned without acquiring the decoder operation lock", name)
	case <-time.After(25 * time.Millisecond):
	}
	decoder.mu.Unlock()

	for range accessors {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("decoder accessor remained blocked after releasing the operation lock")
		}
	}
}
