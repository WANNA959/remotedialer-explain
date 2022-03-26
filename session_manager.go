package remotedialer

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rancher/remotedialer/metrics"
)

type sessionListener interface {
	sessionAdded(clientKey string, sessionKey int64)
	sessionRemoved(clientKey string, sessionKey int64)
}

type sessionManager struct {
	sync.Mutex
	clients map[string][]*Session
	peers   map[string][]*Session
	// 表示client+peers 的所有session
	listeners map[sessionListener]bool
}

func newSessionManager() *sessionManager {
	return &sessionManager{
		clients:   map[string][]*Session{},
		peers:     map[string][]*Session{},
		listeners: map[sessionListener]bool{},
	}
}

// 根据不同的protocol构建 proto + address string 并构建server netConn(session
func toDialer(s *Session, prefix string) Dialer {
	return func(ctx context.Context, proto, address string) (net.Conn, error) {
		fmt.Printf("find dialer session:%+v\n", *s)
		if prefix == "" {
			return s.serverConnectContext(ctx, proto, address)
		}
		// 对应peer中的session.dialer
		return s.serverConnectContext(ctx, prefix+"::"+proto, address)
	}
}

// 删除一个session
func (sm *sessionManager) removeListener(listener sessionListener) {
	sm.Lock()
	defer sm.Unlock()

	delete(sm.listeners, listener)
}

// 添加一个session
func (sm *sessionManager) addListener(listener sessionListener) {
	sm.Lock()
	defer sm.Unlock()

	sm.listeners[listener] = true

	// 同步client+peers clientKey
	// 把client和peer的所有 clientkey-sessionkey 添加newAddClient message
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
}

// find session for a client
func (sm *sessionManager) getDialer(clientKey string) (Dialer, error) {
	sm.Lock()
	defer sm.Unlock()

	// todo LoadBalance?
	// 找到clientkey对应的session 直接return toDialer(sessions[0], "") 第一个session
	sessions := sm.clients[clientKey]
	if len(sessions) > 0 {
		return toDialer(sessions[0], ""), nil
	}

	// 从peers中找
	for _, sessions := range sm.peers {
		for _, session := range sessions {
			session.Lock()
			keys := session.remoteClientKeys[clientKey]
			session.Unlock()
			if len(keys) > 0 {
				return toDialer(session, clientKey), nil
			}
		}
	}

	return nil, fmt.Errorf("failed to find Session for client %s", clientKey)
}

// add websocket session
func (sm *sessionManager) add(clientKey string, conn *websocket.Conn, peer bool) *Session {
	sessionKey := rand.Int63()
	session := newSession(sessionKey, clientKey, conn)

	sm.Lock()
	defer sm.Unlock()

	// 是peer认证通过的 则添加到sm.peers[clientKey]
	if peer {
		sm.peers[clientKey] = append(sm.peers[clientKey], session)
		fmt.Printf("len(peers)=%+v\n", len(sm.peers))
	} else {
		// 是server本身认证通过的， 则添加到sm.clients[clientKey]
		sm.clients[clientKey] = append(sm.clients[clientKey], session)
		fmt.Printf("len(clients)=%+v\n", len(sm.clients))
	}
	metrics.IncSMTotalAddWS(clientKey, peer)

	// l为session
	for l := range sm.listeners {
		l.sessionAdded(clientKey, session.sessionKey)
	}

	return session
}

func (sm *sessionManager) remove(s *Session) {
	var isPeer bool
	sm.Lock()
	defer sm.Unlock()

	for i, store := range []map[string][]*Session{sm.clients, sm.peers} {
		// 对每个clientKey
		var newSessions []*Session
		// 某个clientKey下的若干个session
		for _, v := range store[s.clientKey] {
			if v.sessionKey == s.sessionKey {
				// todo 勘误 clients可能不止一个，故不能用i=0判断是clients or peers
				if i == 0 {
					isPeer = false
				} else {
					isPeer = true
				}
				metrics.IncSMTotalRemoveWS(s.clientKey, isPeer)
				continue
			}
			newSessions = append(newSessions, v)
		}

		// 此处为引用，如可以改变clients or peers
		if len(newSessions) == 0 {
			delete(store, s.clientKey)
		} else {
			store[s.clientKey] = newSessions
		}
	}

	for l := range sm.listeners {
		l.sessionRemoved(s.clientKey, s.sessionKey)
	}

	s.Close()
}
