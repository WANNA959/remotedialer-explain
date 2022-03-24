Reverse Tunneling Dialer
========================

Client makes an outbound connection to a server.  The server can now do net.Dial from the
server that will actually do a net.Dial on the client and pipe all bytes back and forth.

Fun times!

Refer to [`server/`](server/) and [`client/`](client/) how to use.  Or don't.... This framework can hurt your head
trying to conceptualize.

See also:

* [inlets.dev](https://inlets.dev) which uses the client and server components to form a tunnel for clients behind NAT or firewalls.



**HTTP in TCP in Websockets in HTTP in TCP, Tunnel all the things!**

# Usage Demo

## demo code分析

> client

- flag解析命令行参数

| 参数名称 | 参数意义                    |
| -------- | --------------------------- |
| -connect | ws://localhost:8123/connect |
| -id      | clientId（clientKey         |
| -debug   | debug log or not            |



> Server

- flag解析命令行参数

| 参数名称 | 参数意义                                              |
| -------- | ----------------------------------------------------- |
| -listen  | 监听端口                                              |
| -id      | peer id                                               |
| -token   | peer token                                            |
| -peers   | 该server的Peers集合，格式为 id:token:url,id:token:url |
| -debug   | debug log or not                                      |

- 实例化一个server
  - 该server实现了http.handler接口的ServeHTTP方法，所以可作为handler
- 根据peers参数添加peer
  - 要求该server自身也必须有id+token（相互add需要
  - 根据参数实例化peer，添加到peers map
  - go peer.start(ctx, s)
    - 构建向peer请求的client（试图连接其他rancher server
      - header中包含peer的id和token
      - 每5s请求一次

- handler
  - /connect
    - ServeHTTP
      - 认证通过（peer标记是哪种情况认证通过
        - peer=true：id+token为peers中的某一个peer id+token
        - peer=false：该server自定义认证规则
      - 将一个http请求，upgrade为一个websocket请求
      - session := s.sessions.add(clientKey, wsConn, peer)
        - sessionKey := rand.Int63()
        - 创建一个session：session := newSession(sessionKey, clientKey, conn)
        - session_manager
          - peer=true：sm.peers[clientKey] = append(sm.peers[clientKey], session)
          - peer=false：sm.clients[clientKey] = append(sm.clients[clientKey], session)
        - 遍历sm.listeners（本质为若干个session
          - _, err := s.writeMessage(time.Time{}, newAddClient(client))
            - client为clientKey和sessionKey拼接而成
      - session.Serve
  - /client/{id}/{scheme}/{host}{path:.*}：代理转发
    - url := fmt.Sprintf("%s://%s%s", vars["scheme"], vars["host"], vars["path"])
      - scheme表示协议
      - host+path
    - id作为clientkey找到client := getClient(server, clientKey, timeout)
    - resp, err := client.Get(url)
  - /client/{id}/healthz：自定义handleFunc
    - response：resStr := fmt.Sprintf("addrss:%s receive name=%s from client:%s\n", addr, name, id)



## HTTP下

> server

```shell
# 起2个server，必须都带有peer id & peer token，且均在peers中配置，否则handsshake失败
go run ./server/main.go -listen :8123 -id peer0 -token p0token -peers peer1:p1token:ws://localhost:8124/connect

go run ./server/main.go -listen :8124 -id peer1 -token p1token -peers peer0:p0token:ws://localhost:8123/connect

# 起一个没有peer的server3
go run ./server/main.go -listen :8125
```

- 各server详情
  - 还有一个没有加入peers的server3，监听localhost:8125

| sever名称 | server监听地址 |
| --------- | -------------- |
| peer0     | localhost:8123 |
| peer1     | localhost:8124 |

> client

```shell
# client1 connect到peer0，即localhost:8123
go run ./client/main.go -id wanna1 -connect ws://localhost:8123/connect 

# client2 connect到server3，即localhost:8125
go run ./client/main.go -id wanna2 -connect ws://localhost:8125/connect 
```

> 向peer0、peer1请求（wanna1已经connect localhost:8123），peer0和peer1可以发现wanna1 session并转发
>
> 原理：server会从本server+peers server中找wanna这个client key（对应session），故无论对peer0还是peer1请求，都能找到session，剩下的是代理转发工作（是不是peers关系都可）

![image-20220324194941796](/Users/zhujianxing/GoLandProjects/remotedialer/images/image-20220324194941796.png)

> 向peer0、peer1请求server3（向server3请求peer0、peer1）（wanna1已经connect localhost:8123）
>
> peer0和peer1可以发现wanna1 session，并转发到server3
>
> server3不可以发现wanna1 session，故不能转发到peer0 & peer1。这个时候如果通过wanna2 session（即client2，已经connect到server3，则可以请求peer1 & peer2
>
> 原理：同上

![image-20220324200205290](/Users/zhujianxing/GoLandProjects/remotedialer/images/image-20220324200205290.png)

![image-20220324200716965](/Users/zhujianxing/GoLandProjects/remotedialer/images/image-20220324200716965.png)

> 结论

**综上分析，peers的功能就是可以发现peers关系中所有已经connect的clientKey-session，而转发请求可以对任意正在监听的server**

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
