//go:build !ios && (amd64 || arm64)

// Package handles provides a thread-safe handle system for storing Go objects
// that need to be referenced from C callbacks.
//
// When C code needs to reference a Go object (e.g., in callback opaque pointers),
// we cannot store Go pointers directly in C memory. Instead, we register the Go
// object and get back a uintptr handle that can be safely stored in C memory.
//
// This is critical for custom I/O callbacks, interrupt callbacks, and log callbacks.
package handles

import (
	"runtime"
	"sync"
	"sync/atomic"
)

var (
	registry sync.Map
	nextID   atomic.Uintptr
	count    atomic.Int64
)

// Lease owns a registered handle without being retained by the registry.
// Release should be called as soon as the native callback can no longer run.
// If the owner is abandoned, a finalizer unregisters the handle as a best-effort
// fallback; native resources must still be released explicitly by their owner.
type Lease struct {
	mu        sync.Mutex
	id        uintptr
	abandoned func()
}

// RegisterLease stores v and returns a separately owned registration lease.
// abandoned is called by the finalizer, but not by an explicit Release. It must
// not retain the Lease or the object that owns it.
func RegisterLease(v any, abandoned func()) *Lease {
	lease := &Lease{
		id:        Register(v),
		abandoned: abandoned,
	}
	runtime.SetFinalizer(lease, (*Lease).finalize)
	return lease
}

// ID returns the handle ID stored in native callback state.
func (l *Lease) ID() uintptr {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.id
}

// Release unregisters the handle without running the abandonment hook.
// It is safe to call Release multiple times.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	runtime.SetFinalizer(l, nil)
	id, _ := l.take()
	Unregister(id)
}

func (l *Lease) finalize() {
	id, abandoned := l.take()
	Unregister(id)
	if abandoned == nil {
		return
	}
	// A cleanup hook must not be able to panic the runtime finalizer goroutine.
	func() {
		defer func() { _ = recover() }()
		abandoned()
	}()
}

func (l *Lease) take() (uintptr, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := l.id
	abandoned := l.abandoned
	l.id = 0
	l.abandoned = nil
	return id, abandoned
}

// Register stores a Go object and returns a handle ID.
// The handle can be safely stored in C memory (as uintptr or void*).
// The object will remain accessible until Unregister is called.
//
// Thread-safe.
func Register(v any) uintptr {
	id := nextID.Add(1)
	registry.Store(id, v)
	count.Add(1)
	return id
}

// Lookup retrieves a Go object by its handle ID.
// Returns nil if the handle is not registered.
//
// Thread-safe.
func Lookup(id uintptr) any {
	v, _ := registry.Load(id)
	return v
}

// Unregister removes a handle and allows the Go object to be garbage collected.
// Should be called when the C code no longer needs the reference.
//
// Thread-safe.
func Unregister(id uintptr) {
	if _, loaded := registry.LoadAndDelete(id); loaded {
		count.Add(-1)
	}
}

// Take retrieves and unregisters an object in one atomic operation.
// It returns nil if the handle is not registered.
//
// Thread-safe.
func Take(id uintptr) any {
	v, loaded := registry.LoadAndDelete(id)
	if loaded {
		count.Add(-1)
	}
	return v
}

// Count returns the number of currently registered handles.
// Useful for debugging and testing memory leaks.
//
// Thread-safe.
func Count() int {
	return int(count.Load())
}
