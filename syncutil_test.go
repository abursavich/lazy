// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syncutil

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/context"
)

func TestInit(t *testing.T) {
	i := new(Init)
	ctx := context.Background()
	err := errors.New("fail")
	testFunc(t, i, "dedupe failure", ctx, nil, err, func() (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, err
	})
	var val uint32
	testFunc(t, i, "dedupe success", ctx, uint32(1), nil, func() (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return atomic.AddUint32(&val, 1), nil
	})
	testFunc(t, i, "reuse success", ctx, uint32(1), nil, func() (interface{}, error) {
		return atomic.AddUint32(&val, 1), nil
	})
}

func TestInitContext(t *testing.T) {
	i := new(Init)
	bgCtx := context.Background()
	ctx, cancel := context.WithCancel(bgCtx)
	sig := make(chan bool)
	var val uint32
	testFunc(t, i, "cancel context", ctx, nil, context.Canceled, func() (interface{}, error) {
		cancel()
		<-sig
		return atomic.AddUint32(&val, 1), nil
	})
	sig <- true
	testFunc(t, i, "background complete", bgCtx, uint32(1), nil, func() (interface{}, error) {
		return atomic.AddUint32(&val, 42), nil
	})
}

func testFunc(t *testing.T, i *Init, desc string, ctx context.Context, val interface{}, err error, fn func() (interface{}, error)) {
	const N = 10
	type result struct {
		val interface{}
		err error
	}
	ch := make(chan result, N)
	for k := 0; k < N; k++ {
		go func() {
			val, err := i.Do(ctx, fn)
			ch <- result{val, err}
		}()
	}
	for k := 0; k < N; k++ {
		if r := <-ch; r.val != val || r.err != err {
			t.Fatalf("%s: got: (%v, %v); want: (%v, %v)", desc, r.val, r.err, val, err)
		}
	}
}
