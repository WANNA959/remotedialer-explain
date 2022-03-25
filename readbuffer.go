package remotedialer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// 2MB
	MaxBuffer = 1 << 21
)

type readBuffer struct {
	id, readCount, offerCount int64
	cond                      sync.Cond
	// read deadline，在connection中SetReadDeadline
	deadline time.Time
	// read buffer
	buf          bytes.Buffer
	err          error
	backPressure *backPressure
}

func newReadBuffer(id int64, backPressure *backPressure) *readBuffer {
	return &readBuffer{
		id:           id,
		backPressure: backPressure,
		cond: sync.Cond{
			L: &sync.Mutex{},
		},
	}
}

// 用来log
func (r *readBuffer) Status() string {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()
	return fmt.Sprintf("%d/%d", r.readCount, r.offerCount)
}

// reader内容写入到buf
func (r *readBuffer) Offer(reader io.Reader) error {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()

	if r.err != nil {
		return r.err
	}

	// reader写到buf，offset记录buf的offset
	if n, err := io.Copy(&r.buf, reader); err != nil {
		r.offerCount += n
		return err
	} else if n > 0 {
		r.offerCount += n
		r.cond.Broadcast()
	}

	// buf过大 write pause type message
	if r.buf.Len() > MaxBuffer {
		r.backPressure.Pause()
	}

	if r.buf.Len() > MaxBuffer*2 {
		logrus.Errorf("remotedialer buffer exceeded id=%d, length: %d", r.id, r.buf.Len())
	}

	return nil
}

//
func (r *readBuffer) Read(b []byte) (int, error) {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()

	for {
		if r.buf.Len() > 0 {
			n, err := r.buf.Read(b)
			if err != nil {
				// The definition of bytes.Buffer is that this will always return nil because
				// we first checked that bytes.Buffer.Len() > 0. We assume that fact so just assert
				// that here.
				panic("bytes.Buffer returned err=\"" + err.Error() + "\" when buffer length was > 0")
			}
			r.readCount += int64(n)
			r.cond.Broadcast()
			if r.buf.Len() < MaxBuffer/8 {
				r.backPressure.Resume()
			}
			return n, nil
		}

		if r.buf.Cap() > MaxBuffer/8 {
			logrus.Debugf("resetting remotedialer buffer id=%d to zero, old cap %d", r.id, r.buf.Cap())
			r.buf = bytes.Buffer{}
		}

		if r.err != nil {
			return 0, r.err
		}

		now := time.Now()
		if !r.deadline.IsZero() {
			if now.After(r.deadline) {
				return 0, errors.New("deadline exceeded")
			}
		}

		var t *time.Timer
		if !r.deadline.IsZero() {
			t = time.AfterFunc(r.deadline.Sub(now), func() { r.cond.Broadcast() })
		}

		// 阻塞
		r.cond.Wait()
		if t != nil {
			t.Stop()
		}
	}
}

func (r *readBuffer) Close(err error) error {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()
	if r.err == nil {
		r.err = err
	}
	r.cond.Broadcast()
	return nil
}
