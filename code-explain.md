* [Http vs Websocket](#http-vs-websocket)
* [源码分析](#源码分析)
   * [几条路线](#几条路线)
   * [message](#message)
      * [各种类型的message](#各种类型的message)
      * [encode &amp; decode](#encode--decode)
   * [wsconn](#wsconn)
   * [session](#session)
   * [session_manager](#session_manager)
   * [connection](#connection)
      * [Read_buffer](#read_buffer)
         * [back_pressure](#back_pressure)

# Http vs Websocket

- http/https
  - 是单向的，request-response模型
  - 本质是tcp（传输层）对应长连接和短连接、ip（网络层）
- websocket
  - ws/wss
  - **最大区别在于wensocket是全双工、长连接的**
    - 请求-握手-建立连接
    - 双方任意一方断开连接，连接中断
  - 特点：通过网络传输的**任何实时更新或连续数据流**

![img](https://pic3.zhimg.com/80/v2-60317e56734d6fb8c28840bd8c22a952_1440w.jpg)

# 源码分析

## 几条路线

- rancher server（作为handler）——ServeHTTP

  - s.sessions.add（session_controller.go add方法

    - 新建：session := newSession(sessionKey, clientKey, conn)

      - 这个session是server对client的
        - session.client=false

    - 根据认证

      - 本server认证：加入clients
      - peers认证：加入peers

    - 新建的session

      - ```
        for l := range sm.listeners {
           l.sessionAdded(clientKey, session.sessionKey)
        }
        ```

      - 这个sessionAdded本质是对session写AddClient message

        - 对应session另一端收到AddClient message后，会调用s.addRemoteClient(message.address)
          - remoteClientKeys[clientKey]\[sessionKey]=true
          - **而remoteClientKeys会用于某个rancher server判断其peers中是否有某个clientKey 的session**

  - session.Serve

    - 阻塞
    - 有error remove ↓

  - defer s.sessions.remove(session)

    - add的逆操作

- server/main.go中起一个rancher server附带peers参数

  - server.addPeer——go peer.start(ctx, s)

    - session := NewClientSession(func(string, string) bool { return true }, ws)

      - 这个session是server作为client对其他peer的
      - session.client=true

    - session.dialer

      - network=wanna1::tcp address=localhost:8125 parts=[wanna1 tcp]
      - d := s.Dialer(parts[0])：根据parts[0]找到dialer函数
        - d, err := s.sessions.getDialer(clientKey)
          - **根据clientKey遍历clients+peers（通过remoteClientKeys！！！）**找到一个clientKey对应的session
            - toDialer(session, clientKey)
              - serverConnectContext
                - serverConnect
                  - 创建一个connection
                    - **返回的即该connection**
                    - 由此可知session中的conns，保存的是对其他client/server的主动connection（session
                  - **本质是write connect类型message**
                    - 接受者收到connect类型message，调用s.clientConnect(ctx, message)
                      - 新建一个connection：conn := newConnection(message.connID, s, message.proto, message.address)
                      - 加入conns
                      - go clientDial(ctx, s.dialer, conn, message)
                        - 主动连接：netConn, err = d.DialContext(ctx, message.proto, message.address)
                        - pipe
                          - 双工
      - 调用d(ctx, network, address)：d(ctx,"tcp","localhost:8125")

    - addListener

      - sm.listeners[listener] = true

        - 全局唯一添加listener的位置

      - ```
        for k, sessions := range sm.clients {
        		for _, session := range sessions {
        			listener.sessionAdded(k, session.sessionKey)
        		}
        	}
        
        	for k, sessions := range sm.peers {
        		for _, session := range sessions {
        			listener.sessionAdded(k, session.sessionKey)
        		}
        	}
        ```

    - session.Serve

    - removeListener

      - delete(sm.listeners, listener)

- client/main.go：起一个client（这个session client维护

  - 创建一个wensocket conn
    - ws, resp, err := dialer.DialContext(rootCtx, proxyURL, headers)
  - 新建一个session := NewClientSession(auth, ws)
    - session.client=true
  - session.Serve

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

const (
	// auto increase
	Data messageType = iota + 1
	Connect
	Error
	AddClient
	RemoveClient
	Pause
	Resume
)

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

> 详细结构

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

> 方法

- 两种new connection instance
  - NewClientSession
    - NewClientSessionWithDialer
  - newSession

- ping相关
  - startPings
  - stopPings
- serve——持续监听服务
  - serveMessage：decode reader得到message参数，根据不同的参数做对应的操作
    - 收到connect类型的message：创建client端（对接收的server对象） connection
      - clientDial

- server端：Dial
  - serverConnectContext
    - serverConnect
- writeMessage
  - 将一个message 写到websocket conn中
- Close
  - 关闭一个session：stop ping+close conns tunnel并清空conns
- 实现sessionListener接口
  - sessionAdded：写newAddClient message
  - sessionRemoved：写newRemoveClient message

## session_manager

> 详细结构

管理session

- clients
  - 一个client对应多个session
- peers
  - 一个peer对应多个session
- listeners sessionListener-bool
  - removeListener
  - addListener（peer.start中才会调用
    - 每次新建一个到peer的session，需要把该session加到listener中
    - 同时对clients+peers中的所有session，发送newAddClient message
      - 将新的peer clientkey+sessionkey添加

```go
type sessionManager struct {
	sync.Mutex
	clients   map[string][]*Session
	peers     map[string][]*Session
	listeners map[sessionListener]bool
}
```

> 方法

- 定义sessionListener接口（在session中被实现，方便对记录监听的session进行管理
  - sessionAdded
    - newAddClient message
  - sessionRemoved
    - newRemoveClient message

- toDialer：指定session并调用serverConnectContext成员方法
- listeners sessionListener（session）的监控成员修改
  - removeListener
  - addListener
- getDialer：根据clientKey找到一个session
  - 找到一个session，并调用toDialer
    - clients中存在，则从clients中取
    - 否则从peers.remoteClientKeys中找
- 对clients、peers、listeners修改
  - add
    - new session
    - 通过判断是本server or 其他peers认证，加入clients or peers
  - remove

## connection

- 封装了一个session
- readBuffer用来管理connection读
- backPressure用来管理connection写

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

- 当dialer失败，或者连接关闭，处理connection的关闭
  - tunnelClose
    - doTunnelClose
- 实现net.Conn接口
  - Close
  - Read
  - Write
  - LocalAddr
  - RemoteAddr
- 接收到pause/resume/data类型message的操作
  - onPause
  - onResume
  - onData
- 发送pause、resume、Error类型的操作：本质就是write对应类型的message
  - Pause
  - Resume
  - writeErr
- 设置read/write timeout
  - SetDeadline
    - SetReadDeadline
    - SetWriteDeadline

### Read_buffer

> 理解

- 管理connection的read相关操作（read buffer、read deadline等），用connID唯一标识一个read——buffer
- backPressure封装了connection

> 详细结构

- id：conn.id唯一标识

- buf
  - read buffer
  - readCount, offerCount
- deadline
  - 管理connection的read deadline
- sync.Cond
- backPressure封装了connection
  - 主要用来管理connection的写操作（当read buffer过大，需要pause write），阻塞

#### back_pressure

> 理解

主要用来管理connection的写操作（当read buffer过大，需要pause write），阻塞

> 详细结构

- 封装一个connection
  - sync.Cond
  - pause+closed标志