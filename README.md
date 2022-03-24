Reverse Tunneling Dialer
========================

Client makes an outbound connection to a server.  The server can now do net.Dial from the
server that will actually do a net.Dial on the client and pipe all bytes back and forth.

Fun times!

Refer to [`server/`](server/) and [`client/`](client/) how to use.  Or don't.... This framework can hurt your head
trying to conceptualize.

See also:

* [inlets.dev](https://inlets.dev) which uses the client and server components to form a tunnel for clients behind NAT or firewalls.

# Usage Demo



# 源码分析

## message

### 各种类型的message

```go
type message struct {
	id          int64
	err         error
	connID      int64
	messageType messageType
	bytes       []byte
	body        io.Reader
	proto       string
	address     string
}


//data类型
func newMessage(connID int64, bytes []byte) *message {
	return &message{
		id:          nextid(),
		connID:      connID,
		messageType: Data,
		bytes:       bytes, //data
	}
}

// pause类型
func newPause(connID int64) *message {
	return &message{
		id:          nextid(),
		connID:      connID,
		messageType: Pause,
	}
}

//  resume类型
func newResume(connID int64) *message {
	return &message{
		id:          nextid(),
		connID:      connID,
		messageType: Resume,
	}
}

// connect类型 和解码一致：fmt.Sprintf("%s/%s", proto, address)
func newConnect(connID int64, proto, address string) *message {
	return &message{
		id:          nextid(),
		connID:      connID,
		messageType: Connect,
		bytes:       []byte(fmt.Sprintf("%s/%s", proto, address)),
		proto:       proto,
		address:     address,
	}
}

// error类型
func newErrorMessage(connID int64, err error) *message {
	return &message{
		id:          nextid(),
		err:         err,
		connID:      connID,
		messageType: Error,
		bytes:       []byte(err.Error()),
	}
}

// AddClient类型
func newAddClient(client string) *message {
	return &message{
		id:          nextid(),
		messageType: AddClient,
		address:     client,
		bytes:       []byte(client), //clientKey
	}
}

// RemoveClient类型
func newRemoveClient(client string) *message {
	return &message{
		id:          nextid(),
		messageType: RemoveClient,
		address:     client,
		bytes:       []byte(client), //clientKey
	}
}

```

### encode & decode

- decode：newServerMessage
- encode：Bytes+header

## wsconn

封装了一个websocket连接+互斥锁

- newWSConn
  - 实例化一个wsconn，并设置read、ping & pong handler超时时间（setupDeadline
- WriteMessage
  - wsconn write message
- NextReader
  - NextReader returns the next data message received from the peer.

## session

- 管理
  - 一个websocket conn
  - 若干个connection
    - message.connID-connection
      - 关闭：closeConnection
        - conn.tunnelClose(err)
      - client开启：clientConnect
        - go clientDial(ctx, s.dialer, conn, message)
      - server开启：serverConnectContext
        - serverConnect
  - 若干个clientkey
    - clientkey - sessionkey- true
      - removeRemoteClient
      - addRemoteClient
  - 实现sessionListener接口
    - sessionAdded
    - sessionRemoved

```go
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
```

- 两种new connection instance
  - NewClientSessionWithDialer
  - newSession

- ping相关
  - start
  - stop
- 持续监听服务serve
  - serveMessage：decode reader得到message参数，根据不同的参数做对应的操作

## session_manager

- 管理
  - clients
    - 一个client对应多个session
  - peers
    - 一个peer对应多个session
  - listeners sessionListener-bool
    - removeListener
    - addListener

```go
type sessionManager struct {
	sync.Mutex
	clients   map[string][]*Session
	peers     map[string][]*Session
	listeners map[sessionListener]bool
}
```



## connection

- 封装了一个session

```go
type connection struct {
	err           error
	writeDeadline time.Time
	backPressure  *backPressure
	buffer        *readBuffer
	addr          addr
	session       *Session
	connID        int64
}
```



### Read_buffer

### back_pressure

- 封装一个connection
  - sync.Cond
  - pause+closed标志
