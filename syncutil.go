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

// OnceOK is an object that will perform exactly one successful action.
type OnceOK struct {
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
func (o *OnceOK) Do(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	select {
	case <-o.done: // fast path
		return o.val, nil
	default:
	}

	o.once.Do(o.lazyInit)
	errc := make(chan error)
	// register op
	select {
	case <-o.done:
		return o.val, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case o.opc <- op{true, errc, fn}:
		// registered
	}
	// await result
	select {
	case <-o.done:
		return o.val, nil
	case err := <-errc:
		return nil, err
	case <-ctx.Done():
		// quiting
	}
	// unregister op
	select {
	case <-o.done:
		return o.val, nil
	case err := <-errc:
		return nil, err
	case o.opc <- op{false, errc, fn}:
		return nil, ctx.Err()
	}
}

func (o *OnceOK) lazyInit() {
	o.done = make(chan struct{})
	o.opc = make(chan op)
	go o.loop()
}

// loop runs in its own goroutine
func (o *OnceOK) loop() {
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
			close(o.done) // succeeded
			return
		case opv := <-o.opc:
			if !opv.join { // remove pending
				delete(pend, opv.errc)
				continue
			}
			if !busy { // fresh op
				busy = true
				go func() {
					var err error
					o.val, err = opv.fn()
					errc <- err
				}()
			}
			pend[opv.errc] = struct{}{}
		}
	}
}
