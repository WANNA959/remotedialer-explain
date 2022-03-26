package remotedialer

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

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

var (
	idCounter      int64
	legacyDeadline = (15 * time.Second).Milliseconds()
)

func init() {
	r := rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	idCounter = r.Int63()
}

type messageType int64

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

// 产生递增uid
func nextid() int64 {
	return atomic.AddInt64(&idCounter, 1)
}

/*
各种messageType的message instance
*/

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

// 从reader中decode，返回message
func newServerMessage(reader io.Reader) (*message, error) {
	buf := bufio.NewReader(reader)

	//ReadVarint reads an encoded signed integer from r and returns it as an int64.
	// message.id
	id, err := binary.ReadVarint(buf)
	if err != nil {
		return nil, err
	}

	// message.connid
	connID, err := binary.ReadVarint(buf)
	if err != nil {
		return nil, err
	}

	// message.messageType
	mType, err := binary.ReadVarint(buf)
	if err != nil {
		return nil, err
	}

	// 三个 read int64
	// 根据readerdecode build a message
	m := &message{
		id:          id,
		messageType: messageType(mType),
		connID:      connID,
		// 此处为body，不是bytes
		body: buf,
	}

	if m.messageType == Data || m.messageType == Connect {
		// no longer used, this is the deadline field
		_, err := binary.ReadVarint(buf)
		if err != nil {
			return nil, err
		}
	}

	if m.messageType == Connect {
		// 从buf开始读100 Byte
		// message.bytes
		bytes, err := ioutil.ReadAll(io.LimitReader(buf, 100))
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(string(bytes), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed to parse connect address")
		}
		m.proto = parts[0]
		m.address = parts[1]
		m.bytes = bytes
	} else if m.messageType == AddClient || m.messageType == RemoveClient {
		// message.bytes
		bytes, err := ioutil.ReadAll(io.LimitReader(buf, 100))
		if err != nil {
			return nil, err
		}
		m.address = string(bytes)
		m.bytes = bytes
	}

	return m, nil
}

// 从m.body（raeader）返回error类型
func (m *message) Err() error {
	if m.err != nil {
		return m.err
	}
	bytes, err := ioutil.ReadAll(io.LimitReader(m.body, 100))
	if err != nil {
		return err
	}

	str := string(bytes)
	if str == "EOF" {
		m.err = io.EOF
	} else {
		m.err = errors.New(str)
	}
	return m.err
}

func (m *message) Bytes() []byte {
	// encode m to bytes
	return append(m.header(len(m.bytes)), m.bytes...)
}

func (m *message) header(space int) []byte {
	// 24=3*8（三个int64) space为数据部分（m.bytes
	buf := make([]byte, 24+space)
	offset := 0
	//PutVarint encodes an int64 into buf and returns the number of bytes written.
	offset += binary.PutVarint(buf[offset:], m.id)
	offset += binary.PutVarint(buf[offset:], m.connID)
	offset += binary.PutVarint(buf[offset:], int64(m.messageType))
	if m.messageType == Data || m.messageType == Connect {
		// 15s？
		offset += binary.PutVarint(buf[offset:], legacyDeadline)
	}
	return buf[:offset]
}

// 实现reader接口
func (m *message) Read(p []byte) (int, error) {
	//Read reads up to len(p) bytes into p. It returns the number of bytes
	return m.body.Read(p)
}

func (m *message) WriteTo(deadline time.Time, wsConn *wsConn) (int, error) {
	// 将m(encode to bytes)写到websocket中，类型为BinaryMessage
	fmt.Printf("write message:%+v\nbyte=%+v\n", *m, string(m.bytes))
	err := wsConn.WriteMessage(websocket.BinaryMessage, deadline, m.Bytes())
	return len(m.bytes), err
}

// 不同type的message toString
func (m *message) String() string {
	switch m.messageType {
	case Data:
		if m.body == nil {
			return fmt.Sprintf("%d DATA         [%d]: %d bytes: %s", m.id, m.connID, len(m.bytes), string(m.bytes))
		}
		return fmt.Sprintf("%d DATA         [%d]: buffered", m.id, m.connID)
	case Error:
		return fmt.Sprintf("%d ERROR        [%d]: %s", m.id, m.connID, m.Err())
	case Connect:
		return fmt.Sprintf("%d CONNECT      [%d]: %s/%s", m.id, m.connID, m.proto, m.address)
	case AddClient:
		return fmt.Sprintf("%d ADDCLIENT    [%s]", m.id, m.address)
	case RemoveClient:
		return fmt.Sprintf("%d REMOVECLIENT [%s]", m.id, m.address)
	case Pause:
		return fmt.Sprintf("%d PAUSE        [%d]", m.id, m.connID)
	case Resume:
		return fmt.Sprintf("%d RESUME       [%d]", m.id, m.connID)
	}
	return fmt.Sprintf("%d UNKNOWN[%d]: %d", m.id, m.connID, m.messageType)
}
