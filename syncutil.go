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

	"golang.org/x/net/context"
)

// Init is an object that will perform exactly one successful action.
type Init struct {
	once sync.Once
	done chan struct{}
	opc  chan op
	val  interface{}
}

type op struct {
	join bool
	errc chan error
	fn   func() (interface{}, error)
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
	select {
	case <-i.done: // fast path
		return i.val, nil
	default:
	}

	i.once.Do(i.lazyInit)
	errc := make(chan error)
	// register op
	select {
	case <-i.done:
		return i.val, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case i.opc <- op{true, errc, fn}:
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
	// unregister op
	select {
	case <-i.done:
		return i.val, nil
	case err := <-errc:
		return nil, err
	case i.opc <- op{false, errc, fn}:
		return nil, ctx.Err()
	}
}

func (i *Init) lazyInit() {
	i.done = make(chan struct{})
	i.opc = make(chan op)
	go i.loop()
}

// loop runs in its own goroutine
func (i *Init) loop() {
	pend := make(map[chan error]struct{})
	errc := make(chan error)
	busy := false
	for {
		select {
		case err := <-errc: // completed
			if err != nil { // failed
				busy = false
				for c := range pend {
					delete(pend, c)
					c <- err
				}
				continue
			}
			close(i.done) // succeeded
			return
		case o := <-i.opc:
			if !o.join { // remove pending
				delete(pend, o.errc)
				continue
			}
			if !busy { // fresh op
				busy = true
				go func() {
					var err error
					i.val, err = o.fn()
					errc <- err
				}()
			}
			pend[o.errc] = struct{}{}
		}
	}
}
