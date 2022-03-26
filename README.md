# 目录
* [About Reverse Tunneling Dialer](#about-reverse-tunneling-dialer)
* [Usage Demo](#usage-demo)
   * [demo code分析](#demo-code分析)
   * [HTTP下使用](#http下使用)
   * [Todo](#todo)

# 源码分析

[code-explain](code-explain.md)

About Reverse Tunneling Dialer
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

对code不关心可以仅关注命令行参数即可，直接跳到[HTTP下使用](#http下使用)

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

## demo链路跟踪

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

## Todo

 进一步验证session、client等结构和实际连接的对应关系

重点关注session_manager的add、remove方法
