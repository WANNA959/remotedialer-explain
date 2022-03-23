package remotedialer

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

/*
封装了一个websocket连接+互斥
*/
type wsConn struct {
	sync.Mutex
	conn *websocket.Conn
}

func newWSConn(conn *websocket.Conn) *wsConn {
	w := &wsConn{
		conn: conn,
	}
	w.setupDeadline()
	return w
}

func (w *wsConn) WriteMessage(messageType int, deadline time.Time, data []byte) error {
	// 马上write message
	if deadline.IsZero() {
		w.Lock()
		defer w.Unlock()
		return w.conn.WriteMessage(messageType, data)
	}

	// 定时器 write timeout———— io timeout
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		w.Lock()
		defer w.Unlock()
		// nil也是结果
		done <- w.conn.WriteMessage(messageType, data)
	}()

	select {
	// timeout
	case <-ctx.Done():
		return fmt.Errorf("i/o timeout")
	// err or nil（完成
	case err := <-done:
		return err
	}
}

// NextReader returns the next data message received from the peer. The
// returned messageType is either TextMessage or BinaryMessage.
func (w *wsConn) NextReader() (int, io.Reader, error) {
	return w.conn.NextReader()
}

// 设置read、ping & pong handler
func (w *wsConn) setupDeadline() {
	// 设置读操作 timeout 时间=1min
	w.conn.SetReadDeadline(time.Now().Add(PingWaitDuration))
	// SetPingHandler设置从peer接收到的ping消息的处理程序。
	w.conn.SetPingHandler(func(string) error {
		w.Lock()
		// PongMessage表示pong控制消息 timeout=10
		err := w.conn.WriteControl(websocket.PongMessage, []byte(""), time.Now().Add(PingWaitDuration))
		w.Unlock()
		if err != nil {
			return err
		}
		if err := w.conn.SetReadDeadline(time.Now().Add(PingWaitDuration)); err != nil {
			return err
		}
		return w.conn.SetWriteDeadline(time.Now().Add(PingWaitDuration))
	})
	// SetPingHandler设置从peer接收到的pong消息的处理程序。
	w.conn.SetPongHandler(func(string) error {
		if err := w.conn.SetReadDeadline(time.Now().Add(PingWaitDuration)); err != nil {
			return err
		}
		return w.conn.SetWriteDeadline(time.Now().Add(PingWaitDuration))
	})

}
