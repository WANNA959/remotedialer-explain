package remotedialer

import (
	"sync"
)

type backPressure struct {
	cond   sync.Cond
	c      *connection
	paused bool
	closed bool
}

func newBackPressure(c *connection) *backPressure {
	return &backPressure{
		cond: sync.Cond{
			L: &sync.Mutex{},
		},
		c:      c,
		paused: false,
	}
}

/*
对应message的各种type
*/
// pause类型
func (b *backPressure) OnPause() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()

	b.paused = true
	b.cond.Broadcast()
}

// close类型
func (b *backPressure) Close() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()

	b.closed = true
	b.cond.Broadcast()
}

// resume类型
func (b *backPressure) OnResume() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()

	b.paused = false
	b.cond.Broadcast()
}

func (b *backPressure) Pause() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()
	if b.paused {
		return
	}
	b.c.Pause()
	b.paused = true
}

func (b *backPressure) Resume() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()
	if !b.paused {
		return
	}
	b.c.Resume()
	b.paused = false
}

func (b *backPressure) Wait() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()

	for !b.closed && b.paused {
		b.cond.Wait()
	}
}
