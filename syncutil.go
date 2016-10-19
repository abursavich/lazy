// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package syncutil provides additional synchronization primitives on top of
// those provided by the standard library's sync package.
//
// Values containing the types defined in this package should not be copied.
package syncutil

import (
	"sync"
	"sync/atomic"

	"golang.org/x/net/context"
)

const (
	uninitialized = iota
	initialized
	finished
)

// Init is an object that will perform exactly one successful action.
type Init struct {
	mu    sync.Mutex
	state uint32
	done  chan struct{}
	wake  chan struct{}
	errc  chan chan error
	val   interface{}
}

// Do de-duplicates concurrent calls to the function fn and memoizes the
// first result for which a nil error is returned. Calls to Do may return
// before fn is completed if their context ctx is canceled.
//
// Once a call to fn returns, all pending callers share the results. Once a
// call to fn returns with a nil error value, all future callers share the
// results.
//
// The function fn runs in its own goroutine and may complete in the
// background after Do returns. Panics in fn are not recovered.
func (i *Init) Do(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	if s := atomic.LoadUint32(&i.state); s == finished { // fast path
		return i.val, nil
	} else if s == uninitialized { // lazy initialization
		i.mu.Lock()
		if i.state == uninitialized {
			i.done = make(chan struct{})
			i.wake = make(chan struct{}, 1)
			i.errc = make(chan chan error)
			i.wake <- struct{}{}
			atomic.StoreUint32(&i.state, initialized)
		}
		i.mu.Unlock()
	}

	errc := make(chan error)
	// register
	select {
	case <-i.done:
		return i.val, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-i.wake:
		go i.run(errc, fn)
	case i.errc <- errc:
		// registered
	}
	// await result
	select {
	case <-i.done:
		return i.val, nil
	case err := <-errc:
		return nil, err
	case <-ctx.Done():
		// quiting
	}
	// unregister
	select {
	case <-i.done:
		return i.val, nil
	case err := <-errc:
		return nil, err
	case i.errc <- errc:
		return nil, ctx.Err()
	}
}

// run lazily runs in its own goroutine on demand
func (i *Init) run(errc chan error, fn func() (interface{}, error)) {
	c := make(chan error)
	go func() {
		var err error
		i.val, err = fn()
		c <- err
	}()

	m := map[chan error]struct{}{
		errc: struct{}{}, // runner starts registered
	}
	for {
		select {
		case err := <-c:
			if err != nil {
				for errc := range m { // broadcast error
					errc <- err
				}
				i.wake <- struct{}{} // signal next runner
				return
			}
			atomic.StoreUint32(&i.state, finished)
			close(i.done)
			return
		case errc := <-i.errc:
			if _, ok := m[errc]; ok { // unregister
				delete(m, errc)
				continue
			}
			m[errc] = struct{}{} // register
		}
	}
}
