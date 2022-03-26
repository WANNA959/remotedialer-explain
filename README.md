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

- 发送connect请求，注册一个client id—session
- header中包含client id
- remotedialer.ClientConnect(ctx, addr, headers, nil, func(string, string) bool { return true }, nil)——ConnectToProxy
  - ws, resp, err := dialer.DialContext(rootCtx, proxyURL, headers)
  - 新建一个session：session := NewClientSession(auth, ws)

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
    - 根据ws创建一个session
    - s.sessions.addListener(session)
      - sm.listeners[listener] = true
      - 遍历sm.peers+sm.client，添加newAddClient message
        - 接收newAddClient message处理：addRemoteClient—处理remoteClientKeys

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

## HTTP下使用

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

![image-20220324194941796](https://tva1.sinaimg.cn/large/e6c9d24ely1h0lf3hif3dj219q0ig42j.jpg)

> 向peer0、peer1请求server3（向server3请求peer0、peer1）（wanna1已经connect localhost:8123）
>
> peer0和peer1可以发现wanna1 session，并转发到server3
>
> server3不可以发现wanna1 session，故不能转发到peer0 & peer1。这个时候如果通过wanna2 session（即client2，已经connect到server3，则可以请求peer1 & peer2
>
> 原理：同上

![image-20220324200205290](https://tva1.sinaimg.cn/large/e6c9d24ely1h0lf3ifvegj219i0iogpx.jpg)

![image-20220324200716965](https://tva1.sinaimg.cn/large/e6c9d24ely1h0lf3kdhkmj219y0a0q4u.jpg)

> 结论

**综上分析，peers的功能就是可以发现peers关系中所有已经connect的clientKey-session，而转发请求可以对任意正在监听的server**

## Todo

 进一步验证session、client等结构和实际连接的对应关系

重点关注session_manager的add、remove方法

```
# 操作：启动peer0 peer1 server2
peer0 8123
peer1 8124
server2 8125

peer0
write ADDCLIENT address:peer1/5577006791947779410
get ADDCLIENT [peer0/5577006791947779410]

peer1 
write ADDCLIENT address:peer1/5577006791947779410
get ADDCLIENT [peer0/5577006791947779410]
均加入到peers中 len(peers)=1

# 操作client connect peer0
client connect peer0 

peer0 
handle connect len(clients)=1 
write ADDCLIENT wanna1/4037200794235010051

peer1
get ADDCLIENT [wanna1/4037200794235010051]

# 操作postman 请求  ws://localhost:8124/client/wanna1/http/localhost:8125/client/wanna1/healthz?name=zhujian&timeout=1
具体交互过程如下面时序图所示

peer1 
接受 http请求：/client/wanna1/http/localhost:8125/client/wanna1/healthz
write Connect 9075334938635334801 to peer0 byte=wanna1::tcp/localhost:8125
write Data 9075334938635334802 to peer0 
get Data 6457154278583150539 from peer0

peer0 
get Connect 9075334938635334801 from peer1 
write Connect 6457154278583150537 to client
get Data 9075334938635334802 from peer1
write Data 6457154278583150538 to client
get Data 1178506748663006651 from client
write Data 6457154278583150539 to peer1

client 
get CONNECT 6457154278583150537 from peer0
get Data 6457154278583150538 from peer0
write 1178506748663006651 to peer0

server2 
接受 http请求：/client/wanna1/healthz 
```

![时序图](https://tva1.sinaimg.cn/large/e6c9d24ely1h0n9k3gylij21b50u0gog.jpg)

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
