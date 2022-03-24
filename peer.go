package remotedialer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rancher/remotedialer/metrics"
	"github.com/sirupsen/logrus"
)

var (
	Token = "X-API-Tunnel-Token"
	ID    = "X-API-Tunnel-ID"
)

/*
创建一个peer
若存在
	不等于新创建的peer，cancel
	等于，直接return
不存在，start
*/
func (s *Server) AddPeer(url, id, token string) {
	if s.PeerID == "" || s.PeerToken == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	peer := peer{
		url:    url,
		id:     id,
		token:  token,
		cancel: cancel,
	}

	logrus.Infof("Adding peer %s, %s", url, id)

	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	// 判断是否相等
	if p, ok := s.peers[id]; ok {
		if p.equals(peer) {
			return
		}
		// 不相等cancel
		p.cancel()
	}

	s.peers[id] = peer
	go peer.start(ctx, s)
}

//
func (s *Server) RemovePeer(id string) {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	if p, ok := s.peers[id]; ok {
		logrus.Infof("Removing peer %s", id)
		p.cancel()
	}
	delete(s.peers, id)
}

type peer struct {
	url, id, token string
	cancel         func()
}

func (p peer) equals(other peer) bool {
	return p.url == other.url &&
		p.id == other.id &&
		p.token == other.token
}

func (p *peer) start(ctx context.Context, s *Server) {
	// 创建header
	headers := http.Header{
		ID:    {s.PeerID},
		Token: {s.PeerToken},
	}

	// 创建websocket client dialer
	dialer := &websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		HandshakeTimeout: HandshakeTimeOut,
	}

outer:
	for {
		select {
		case <-ctx.Done():
			break outer
		default:
		}

		// 尝试和websocket建立连接
		metrics.IncSMTotalAddPeerAttempt(p.id)
		ws, _, err := dialer.Dial(p.url, headers)
		if err != nil {
			logrus.Errorf("Failed to connect to peer %s [local ID=%s]: %v", p.url, s.PeerID, err)
			time.Sleep(5 * time.Second)
			continue
		}
		// 成功建立websocket连接
		metrics.IncSMTotalPeerConnected(p.id)

		//
		session := NewClientSession(func(string, string) bool { return true }, ws)
		session.dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			parts := strings.SplitN(network, "::", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid clientKey/proto: %s", network)
			}
			d := s.Dialer(parts[0])
			return d(ctx, parts[1], address)
		}

		s.sessions.addListener(session)
		_, err = session.Serve(ctx)
		s.sessions.removeListener(session)
		session.Close()

		if err != nil {
			logrus.Errorf("Failed to serve peer connection %s: %v", p.id, err)
		}

		ws.Close()
		time.Sleep(5 * time.Second)
	}
}
