package rpc

import (
	"net"
	"net/rpc"
	"time"

	"github.com/sirupsen/logrus"
)

type RpcNode struct {
	listener  net.Listener
	listening bool
	server    *rpc.Server
	ip        string
}

func (rpcNode *RpcNode) Register(serverName string, server interface{}) error {
	rpcNode.server = rpc.NewServer()
	if err := rpcNode.server.RegisterName(serverName, server); err != nil {
		logrus.Error("[", rpcNode.ip, "] register error:", err)
		return err
	}
	return nil
}

func (rpcNode *RpcNode) Serve(addr string, run chan bool) error {
	var err error
	rpcNode.listener, err = net.Listen("tcp", addr)
	if err != nil {
		logrus.Error("[", rpcNode.ip, "] listen error: ", err)
		return err
	}
	rpcNode.listening = true
	rpcNode.ip = addr
	go func() {
		for rpcNode.listening {
			conn, err := rpcNode.listener.Accept()
			if err != nil {
				logrus.Error("[", rpcNode.ip, "] accept error: ", err)
				return
			}
			go rpcNode.server.ServeConn(conn)
		}
	}()
	close(run)
	return nil
}

func (rpcNode *RpcNode) StopServe() {
	rpcNode.listening = false
	rpcNode.listener.Close()
}

// RemoteCall calls the RPC method at addr.
//
// Note: An empty interface can hold values of any type. (https://tour.golang.org/methods/14)
// Re-connect to the client every time can be slow. You can use connection pool to improve the performance.
func (rpcNode *RpcNode) RemoteCall(addr string, method string, args interface{}, reply interface{}) error {
	if method != "Chord.Ping" {
		logrus.Infof("[%s] RemoteCall %s %s %v", rpcNode.ip, addr, method, args)
	}
	// Note: Here we use DialTimeout to set a timeout of 10 seconds.
	conn, err := net.DialTimeout("tcp", addr, 200*time.Second)
	if err != nil {
		logrus.Error("[", rpcNode.ip, "]", " [", method, "] dialing: ", err)
		return err
	}
	client := rpc.NewClient(conn)
	defer client.Close()
	err = client.Call(method, args, reply)
	if err != nil {
		logrus.Error("[", rpcNode.ip, "]", " [", method, "] RemoteCall error: ", err)
		return err
	}
	return nil
}
