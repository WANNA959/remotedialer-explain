package remotedialer

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

func clientDial(ctx context.Context, dialer Dialer, conn *connection, message *message) {
	defer conn.Close()

	var (
		netConn net.Conn
		err     error
	)

	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Minute))
	// dial proto address
	// get  dial server
	if dialer == nil {
		d := net.Dialer{}
		// DialContext connects to the address on the named network using
		// the provided context.

		netConn, err = d.DialContext(ctx, message.proto, message.address)
	} else {
		netConn, err = dialer(ctx, message.proto, message.address)
	}
	cancel()

	if err != nil {
		conn.tunnelClose(err)
		return
	}
	defer netConn.Close()

	pipe(conn, netConn)
}

func pipe(client *connection, server net.Conn) {
	wg := sync.WaitGroup{}
	wg.Add(1)

	// 处理err
	close := func(err error) error {
		if err == nil {
			err = io.EOF
		}
		client.doTunnelClose(err)
		server.Close()
		return err
	}

	// 起一个协程，监控server发送给client的消息
	go func() {
		defer wg.Done()
		//Copy(dst Writer, src Reader)
		_, err := io.Copy(server, client)
		close(err)
	}()

	// client发送给server消息
	_, err := io.Copy(client, server)
	err = close(err)
	// 阻塞
	wg.Wait()

	// Write tunnel error after no more I/O is happening, just incase messages get out of order
	client.writeErr(err)
}
