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
// on pause类型：表示收到pause 类型message的操作
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

// on resume类型：表示收到resume 类型message的操作
func (b *backPressure) OnResume() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()

	b.paused = false
	b.cond.Broadcast()
}

// 发送pause 类型message的操作
func (b *backPressure) Pause() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()
	// 已经发送过pause了
	if b.paused {
		return
	}
	b.c.Pause()
	b.paused = true
}

// 发送resume 类型message的操作
func (b *backPressure) Resume() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()
	// 已经发送过resume类型了
	if !b.paused {
		return
	}
	b.c.Resume()
	b.paused = false
}

func (b *backPressure) Wait() {
	b.cond.L.Lock()
	defer b.cond.L.Unlock()

	// 没有关闭且处理pause状态的connection，阻塞
	for !b.closed && b.paused {
		b.cond.Wait()
	}
}
