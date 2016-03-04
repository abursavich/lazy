package syncutil

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/context"
)

func TestOnceOK(t *testing.T) {
	o := new(OnceOK)
	ctx := context.Background()
	err := errors.New("fail")
	testFunc(t, o, "dedupe failure", ctx, nil, err, func() (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, err
	})
	var val uint32
	testFunc(t, o, "dedupe success", ctx, uint32(1), nil, func() (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return atomic.AddUint32(&val, 1), nil
	})
	testFunc(t, o, "reuse success", ctx, uint32(1), nil, func() (interface{}, error) {
		return atomic.AddUint32(&val, 1), nil
	})
}

func TestOnceOKContext(t *testing.T) {
	o := new(OnceOK)
	bgCtx := context.Background()
	ctx, cancel := context.WithCancel(bgCtx)
	sig := make(chan bool)
	var val uint32
	testFunc(t, o, "cancel context", ctx, nil, context.Canceled, func() (interface{}, error) {
		cancel()
		<-sig
		return atomic.AddUint32(&val, 1), nil
	})
	sig <- true
	testFunc(t, o, "background complete", bgCtx, uint32(1), nil, func() (interface{}, error) {
		return atomic.AddUint32(&val, 42), nil
	})
}

func testFunc(t *testing.T, o *OnceOK, desc string, ctx context.Context, val interface{}, err error, fn func() (interface{}, error)) {
	const N = 10
	type result struct {
		val interface{}
		err error
	}
	ch := make(chan result, N)
	for i := 0; i < N; i++ {
		go func() {
			val, err := o.Do(ctx, fn)
			ch <- result{val, err}
		}()
	}
	for i := 0; i < N; i++ {
		if r := <-ch; r.val != val || r.err != err {
			t.Fatalf("%s: got: (%v, %v); want: (%v, %v)", desc, r.val, r.err, val, err)
		}
	}
}
