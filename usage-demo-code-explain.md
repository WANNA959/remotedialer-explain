# Usage Demo源码分析

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