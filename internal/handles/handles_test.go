package handles

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestRegisterAndLookup(t *testing.T) {
	type testData struct {
		Name  string
		Value int
	}

	data := &testData{Name: "test", Value: 42}
	handle := Register(data)

	if handle == 0 {
		t.Error("Register should return non-zero handle")
	}

	got := Lookup(handle)
	if got == nil {
		t.Error("Lookup should return non-nil value")
	}

	gotData, ok := got.(*testData)
	if !ok {
		t.Errorf("Lookup returned wrong type: %T", got)
	}

	if gotData.Name != "test" || gotData.Value != 42 {
		t.Errorf("Lookup returned wrong data: %+v", gotData)
	}
}

func TestUnregister(t *testing.T) {
	data := "test string"
	handle := Register(data)

	// Verify it's registered
	if Lookup(handle) == nil {
		t.Error("Expected value before Unregister")
	}

	// Unregister
	Unregister(handle)

	// Verify it's gone
	if Lookup(handle) != nil {
		t.Error("Expected nil after Unregister")
	}
}

func TestTake(t *testing.T) {
	handle := Register("value")

	if got := Take(handle); got != "value" {
		t.Fatalf("Take returned %v, want value", got)
	}
	if got := Lookup(handle); got != nil {
		t.Fatalf("Lookup after Take returned %v, want nil", got)
	}
	if got := Take(handle); got != nil {
		t.Fatalf("second Take returned %v, want nil", got)
	}
}

func TestLookupNonExistent(t *testing.T) {
	got := Lookup(999999)
	if got != nil {
		t.Error("Lookup of non-existent handle should return nil")
	}
}

func TestConcurrentAccess(t *testing.T) {
	const numGoroutines = 100
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				data := struct {
					ID  int
					Seq int
				}{id, j}
				handle := Register(&data)
				got := Lookup(handle)
				if got == nil {
					t.Errorf("Lookup returned nil for handle %d", handle)
				}
				Unregister(handle)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentTakeReturnsValueOnce(t *testing.T) {
	const numGoroutines = 100

	baseline := Count()
	handle := Register("value")
	start := make(chan struct{})
	results := make(chan any, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			<-start
			results <- Take(handle)
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	winners := 0
	for result := range results {
		if result != nil {
			winners++
			if result != "value" {
				t.Fatalf("Take returned %v, want value", result)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("successful Take calls = %d, want 1", winners)
	}
	if got := Count(); got != baseline {
		t.Fatalf("registered handles = %d, want baseline %d", got, baseline)
	}
}

func TestConcurrentRegistrationCount(t *testing.T) {
	const numHandles = 1000

	baseline := Count()
	handles := make(chan uintptr, numHandles)

	var registerWG sync.WaitGroup
	registerWG.Add(numHandles)
	for i := range numHandles {
		go func(value int) {
			defer registerWG.Done()
			handles <- Register(value)
		}(i)
	}
	registerWG.Wait()
	close(handles)

	if got := Count(); got != baseline+numHandles {
		t.Fatalf("registered handles = %d, want %d", got, baseline+numHandles)
	}

	var unregisterWG sync.WaitGroup
	for handle := range handles {
		unregisterWG.Add(1)
		go func() {
			defer unregisterWG.Done()
			Unregister(handle)
		}()
	}
	unregisterWG.Wait()

	if got := Count(); got != baseline {
		t.Fatalf("registered handles = %d, want baseline %d", got, baseline)
	}
}

func TestHandlesAreUnique(t *testing.T) {
	handles := make(map[uintptr]bool)

	for i := 0; i < 1000; i++ {
		h := Register(i)
		if handles[h] {
			t.Errorf("Handle %d was returned twice", h)
		}
		handles[h] = true
	}

	// Clean up
	for h := range handles {
		Unregister(h)
	}
}

func TestLeaseReleaseIsExplicitAndIdempotent(t *testing.T) {
	baseline := Count()
	abandoned := false
	lease := RegisterLease("value", func() { abandoned = true })
	id := lease.ID()

	if got := Lookup(id); got != "value" {
		t.Fatalf("Lookup(%d) = %v, want value", id, got)
	}
	lease.Release()
	lease.Release()

	if abandoned {
		t.Fatal("explicit Release invoked the abandonment hook")
	}
	if got := Lookup(id); got != nil {
		t.Fatalf("Lookup(%d) after Release = %v, want nil", id, got)
	}
	if got := Count(); got != baseline {
		t.Fatalf("registered handles = %d, want baseline %d", got, baseline)
	}
}

func TestLeaseFinalizerReleasesAbandonedRegistration(t *testing.T) {
	baseline := Count()
	abandoned := make(chan struct{})
	id := registerAbandonedLease(abandoned)

	deadline := time.Now().Add(5 * time.Second)
	for Lookup(id) != nil && time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}

	if got := Lookup(id); got != nil {
		Unregister(id)
		t.Fatalf("Lookup(%d) after garbage collection = %v, want nil", id, got)
	}
	select {
	case <-abandoned:
	case <-time.After(time.Second):
		t.Fatal("lease finalizer did not invoke the abandonment hook")
	}
	if got := Count(); got != baseline {
		t.Fatalf("registered handles = %d, want baseline %d", got, baseline)
	}
}

func registerAbandonedLease(abandoned chan<- struct{}) uintptr {
	lease := RegisterLease("value", func() { close(abandoned) })
	id := lease.ID()
	runtime.KeepAlive(lease)
	return id
}
