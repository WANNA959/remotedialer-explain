package remotedialer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Session struct {
	sync.Mutex

	nextConnID       int64
	clientKey        string
	sessionKey       int64
	conn             *wsConn
	conns            map[int64]*connection
	remoteClientKeys map[string]map[int]bool
	auth             ConnectAuthorizer

	pingCancel context.CancelFunc
	pingWait   sync.WaitGroup

	dialer Dialer
	client bool
}

// PrintTunnelData No tunnel logging by default
var PrintTunnelData bool

func init() {
	//PrintTunnelData = true
	if os.Getenv("CATTLE_TUNNEL_DATA_DEBUG") == "true" {
		PrintTunnelData = true
	}
}

func NewClientSession(auth ConnectAuthorizer, conn *websocket.Conn) *Session {
	return NewClientSessionWithDialer(auth, conn, nil)
}

// 两种new session
func NewClientSessionWithDialer(auth ConnectAuthorizer, conn *websocket.Conn, dialer Dialer) *Session {
	return &Session{
		clientKey: "client",
		conn:      newWSConn(conn),
		conns:     map[int64]*connection{},
		auth:      auth,
		client:    true,
		dialer:    dialer,
	}
}

func newSession(sessionKey int64, clientKey string, conn *websocket.Conn) *Session {
	return &Session{
		nextConnID:       1,
		clientKey:        clientKey,
		sessionKey:       sessionKey,
		conn:             newWSConn(conn),
		conns:            map[int64]*connection{},
		remoteClientKeys: map[string]map[int]bool{},
	}
}

// 起一个协程 ping/5s
func (s *Session) startPings(rootCtx context.Context) {
	ctx, cancel := context.WithCancel(rootCtx)
	s.pingCancel = cancel
	// 同步
	s.pingWait.Add(1)

	// 起一个goroutine，5s websocket ping一次
	go func() {
		// 同步close ping 函数
		defer s.pingWait.Done()

		t := time.NewTicker(PingWriteInterval)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.conn.Lock()
				// ping
				if err := s.conn.conn.WriteControl(websocket.PingMessage, []byte(""), time.Now().Add(PingWaitDuration)); err != nil {
					logrus.WithError(err).Error("Error writing ping")
				}
				logrus.Debug("Wrote ping")
				s.conn.Unlock()
			}
		}
	}()
}

func (s *Session) stopPings() {
	if s.pingCancel == nil {
		return
	}

	s.pingCancel()
	// 同步ping 协程优雅退出
	s.pingWait.Wait()
}

func (s *Session) Serve(ctx context.Context) (int, error) {
	// client start ping server
	if s.client {
		s.startPings(ctx)
	}

	// 持续serve
	for {
		// mytype = TextMessage or BinaryMessage
		fmt.Println("serve")
		msType, reader, err := s.conn.NextReader()
		//TextMessage = 1 BinaryMessage = 2
		fmt.Printf("msType=%d\n", msType)
		//fmt.Println(msType)
		if err != nil {
			return 400, err
		}

		//TextMessage
		if msType != websocket.BinaryMessage {
			return 400, errWrongMessageType
		}

		if err := s.serveMessage(ctx, reader); err != nil {
			return 500, err
		}
	}
}

func (s *Session) serveMessage(ctx context.Context, reader io.Reader) error {

	// 解码 得到request message
	message, err := newServerMessage(reader)
	fmt.Printf("message:%+v\n", message)
	if err != nil {
		return err
	}

	if PrintTunnelData {
		logrus.Debug("REQUEST ", message)
	}

	if message.messageType == Connect {
		// 认证
		if s.auth == nil || !s.auth(message.proto, message.address) {
			return errors.New("connect not allowed")
		}
		s.clientConnect(ctx, message)
		return nil
	}

	s.Lock()
	if message.messageType == AddClient && s.remoteClientKeys != nil {
		err := s.addRemoteClient(message.address)
		s.Unlock()
		return err
	} else if message.messageType == RemoveClient {
		err := s.removeRemoteClient(message.address)
		s.Unlock()
		return err
	}
	conn := s.conns[message.connID]
	s.Unlock()

	if conn == nil {
		if message.messageType == Data {
			err := fmt.Errorf("connection not found %s/%d/%d", s.clientKey, s.sessionKey, message.connID)
			newErrorMessage(message.connID, err).WriteTo(defaultDeadline(), s.conn)
		}
		return nil
	}

	// conn!=nil
	switch message.messageType {
	case Data:
		if err := conn.OnData(message); err != nil {
			s.closeConnection(message.connID, err)
		}
	case Pause:
		conn.OnPause()
	case Resume:
		conn.OnResume()
	case Error:
		s.closeConnection(message.connID, message.Err())
	}

	return nil
}

func defaultDeadline() time.Time {
	return time.Now().Add(time.Minute)
}

// parse Address to clientKey, sessionKey
func parseAddress(address string) (string, int, error) {
	parts := strings.SplitN(address, "/", 2)
	if len(parts) != 2 {
		return "", 0, errors.New("not / separated")
	}
	v, err := strconv.Atoi(parts[1])
	return parts[0], v, err
}

// 添加 clientKey - sessionKey - true
func (s *Session) addRemoteClient(address string) error {
	clientKey, sessionKey, err := parseAddress(address)
	if err != nil {
		return fmt.Errorf("invalid remote Session %s: %v", address, err)
	}

	// clientKey不存在则创建  clientKey - map[int]bool{}
	keys := s.remoteClientKeys[clientKey]
	if keys == nil {
		keys = map[int]bool{}
		s.remoteClientKeys[clientKey] = keys
	}
	// clientKey - sessionKey - true
	keys[sessionKey] = true

	if PrintTunnelData {
		logrus.Debugf("ADD REMOTE CLIENT %s, SESSION %d", address, s.sessionKey)
	}

	return nil
}

// 删除  clientKey - sessionKey - true
func (s *Session) removeRemoteClient(address string) error {
	clientKey, sessionKey, err := parseAddress(address)
	if err != nil {
		return fmt.Errorf("invalid remote Session %s: %v", address, err)
	}

	keys := s.remoteClientKeys[clientKey]
	delete(keys, int(sessionKey))
	if len(keys) == 0 {
		delete(s.remoteClientKeys, clientKey)
	}

	if PrintTunnelData {
		logrus.Debugf("REMOVE REMOTE CLIENT %s, SESSION %d", address, s.sessionKey)
	}

	return nil
}

// 从conns删除一个conn
func (s *Session) closeConnection(connID int64, err error) {
	s.Lock()
	conn := s.conns[connID]
	delete(s.conns, connID)
	if PrintTunnelData {
		logrus.Debugf("CONNECTIONS %d %d", s.sessionKey, len(s.conns))
	}
	s.Unlock()

	if conn != nil {
		conn.tunnelClose(err)
	}
}

// client端:增加一个conn到conns
func (s *Session) clientConnect(ctx context.Context, message *message) {
	conn := newConnection(message.connID, s, message.proto, message.address)

	s.Lock()
	s.conns[message.connID] = conn
	if PrintTunnelData {
		logrus.Debugf("CONNECTIONS %d %d", s.sessionKey, len(s.conns))
	}
	s.Unlock()

	fmt.Println("client dial here")
	go clientDial(ctx, s.dialer, conn, message)
}

type connResult struct {
	conn net.Conn
	err  error
}

func (s *Session) Dial(ctx context.Context, proto, address string) (net.Conn, error) {
	return s.serverConnectContext(ctx, proto, address)
}

// server端:增加一个conn到conns
func (s *Session) serverConnectContext(ctx context.Context, proto, address string) (net.Conn, error) {
	// Deadline returns the time when work done on behalf of this context should be canceled.
	// ok=false代表no deadline is set
	deadline, ok := ctx.Deadline()
	if ok {
		// 设置了deadline
		return s.serverConnect(deadline, proto, address)
	}

	result := make(chan connResult, 1)
	// 没有设置deadline，用default deadline
	go func() {
		c, err := s.serverConnect(defaultDeadline(), proto, address)
		result <- connResult{conn: c, err: err}
	}()

	select {
	// timeout
	case <-ctx.Done():
		// We don't want to orphan an open connection so we wait for the result and immediately close it
		go func() {
			// timeout，但是为了new connection可管理（不是orphan），等待结果并立马close
			r := <-result
			if r.err == nil {
				r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	// 得到connResult
	case r := <-result:
		return r.conn, r.err
	}
}

func (s *Session) serverConnect(deadline time.Time, proto, address string) (net.Conn, error) {
	connID := atomic.AddInt64(&s.nextConnID, 1)
	conn := newConnection(connID, s, proto, address)

	s.Lock()
	s.conns[connID] = conn
	if PrintTunnelData {
		logrus.Debugf("CONNECTIONS %d %d", s.sessionKey, len(s.conns))
	}
	s.Unlock()

	// Connect类型的message
	_, err := s.writeMessage(deadline, newConnect(connID, proto, address))
	if err != nil {
		s.closeConnection(connID, err)
		return nil, err
	}

	return conn, err
}

// 写到websocket conn中
func (s *Session) writeMessage(deadline time.Time, message *message) (int, error) {
	if PrintTunnelData {
		logrus.Debug("WRITE ", message)
	}
	return message.WriteTo(deadline, s.conn)
}

//关闭一个session：stopping+close conns tunnel并清空conns
func (s *Session) Close() {
	s.Lock()
	defer s.Unlock()

	s.stopPings()

	for _, connection := range s.conns {
		connection.tunnelClose(errors.New("tunnel disconnect"))
	}

	s.conns = map[int64]*connection{}
}

/*
实现了 session_maneger sessionListener interface

有err close websocket conn
*/
func (s *Session) sessionAdded(clientKey string, sessionKey int64) {
	client := fmt.Sprintf("%s/%d", clientKey, sessionKey)
	_, err := s.writeMessage(time.Time{}, newAddClient(client))
	if err != nil {
		s.conn.conn.Close()
	}
}

func (s *Session) sessionRemoved(clientKey string, sessionKey int64) {
	client := fmt.Sprintf("%s/%d", clientKey, sessionKey)
	_, err := s.writeMessage(time.Time{}, newRemoveClient(client))
	if err != nil {
		s.conn.conn.Close()
	}
}
