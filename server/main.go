package main

import (
	"flag"
	"fmt"
	"github.com/rancher/remotedialer/common"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/rancher/remotedialer"
	"github.com/sirupsen/logrus"
)

var (
	clients = map[string]*http.Client{}
	l       sync.Mutex
	counter int64
)

// 自定义 认证方案
func authorizer(req *http.Request) (string, bool, error) {
	id := req.Header.Get(common.TUNNELID)
	// id!=""即pass【
	return id, id != "", nil
}

func Client(server *remotedialer.Server, rw http.ResponseWriter, req *http.Request) {
	timeout := req.URL.Query().Get("timeout")
	name := req.URL.Query().Get("name")
	fmt.Printf("name=%s", name)
	if timeout == "" {
		timeout = "15"
	}

	// 解析参数为map形式 /client/{id}/{scheme}/{host}{path:.*}
	vars := mux.Vars(req)
	clientKey := vars["id"]
	url := fmt.Sprintf("%s://%s%s", vars["scheme"], vars["host"], vars["path"])
	fmt.Printf("request url = %s\n", url)

	// get/set client
	client := getClient(server, clientKey, timeout)

	id := atomic.AddInt64(&counter, 1)
	logrus.Infof("[%03d] REQ t=%s %s", id, timeout, url)

	resp, err := client.Get(url)
	if err != nil {
		logrus.Errorf("[%03d] REQ ERR t=%s %s: %v", id, timeout, url, err)
		remotedialer.DefaultErrorWriter(rw, req, 500, err)
		return
	}
	defer resp.Body.Close()

	logrus.Infof("[%03d] REQ OK t=%s %s", id, timeout, url)
	rw.WriteHeader(resp.StatusCode)
	io.Copy(rw, resp.Body)
	logrus.Infof("[%03d] REQ DONE t=%s %s", id, timeout, url)
}

/*
get / set方法
根据clientkey找到对应的client  *http.Client{}
不存在，则创建client，使得server可以作为client通信
*/
func getClient(server *remotedialer.Server, clientKey, timeout string) *http.Client {
	l.Lock()
	defer l.Unlock()

	key := fmt.Sprintf("%s/%s", clientKey, timeout)
	client := clients[key]
	// 存在 直接返回
	if client != nil {
		return client
	}
	// 不存在，根据clientkey build(前提是有这个clientkey对应的session
	dialer := server.Dialer(clientKey)
	client = &http.Client{
		Transport: &http.Transport{
			DialContext: dialer,
		},
	}
	if timeout != "" {
		t, err := strconv.Atoi(timeout)
		if err == nil {
			client.Timeout = time.Duration(t) * time.Second
		}
	}

	//set client
	clients[key] = client
	return client
}

func main() {
	var (
		addr      string
		peerID    string
		peerToken string
		peers     string
		debug     bool
	)
	flag.StringVar(&addr, "listen", ":8123", "Listen address")
	flag.StringVar(&peerID, "id", "", "Peer ID")
	flag.StringVar(&peerToken, "token", "", "Peer Token")
	flag.StringVar(&peers, "peers", "", "Peers format id:token:url,id:token:url")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	flag.Parse()

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
		remotedialer.PrintTunnelData = true
	}

	// 实例化一个server（实现了handler接口
	handler := remotedialer.New(authorizer, remotedialer.DefaultErrorWriter)
	handler.PeerToken = peerToken
	handler.PeerID = peerID

	for _, peer := range strings.Split(peers, ",") {
		// id token url
		parts := strings.SplitN(strings.TrimSpace(peer), ":", 3)
		fmt.Printf("peer parts:%+v\n", parts)
		if len(parts) != 3 {
			continue
		}
		handler.AddPeer(parts[2], parts[0], parts[1])
	}

	router := mux.NewRouter()
	// 处理连接请求 如demo中client / main
	router.Handle("/connect", handler)

	// 代理转发，四个参数
	router.HandleFunc("/client/{id}/{scheme}/{host}{path:.*}", func(rw http.ResponseWriter, req *http.Request) {
		fmt.Println(req.URL.Path)
		Client(handler, rw, req)
	})

	//自定义一个handlefunc
	router.HandleFunc("/client/{id}/healthz", func(writer http.ResponseWriter, request *http.Request) {
		name := request.URL.Query().Get("name")
		vars := mux.Vars(request)
		id := vars["id"]
		resStr := fmt.Sprintf("addrss:%s receive name=%s from client:%s\n", addr, name, id)
		fmt.Printf(resStr)
		writer.Write([]byte(resStr))
	})

	//自定义一个handlefunc
	router.HandleFunc("/client/healthz", func(writer http.ResponseWriter, request *http.Request) {
		name := request.URL.Query().Get("name")
		resStr := fmt.Sprintf("addrss:%s receive name=%s\n", addr, name)
		fmt.Printf(resStr)
		writer.Write([]byte(resStr))
	})

	err := http.ListenAndServe(addr, router)
	if err != nil {
		fmt.Printf("ListenAndServe err:%+v", err)
		return
	}
	fmt.Println("Listening on ", addr)
}
