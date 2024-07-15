package kademlia

import (
	"errors"
	"math/big"
)

type RpcPair struct {
	Addr string
	Info any
}

type RpcInterface struct {
	node *Node
}

func (rpc *RpcInterface) FindNode(request RpcPair, reply *[]string) error {
	*reply = rpc.node.FindNode(request.Info.(*big.Int))
	if rpc.node.addr != request.Addr {
		rpc.node.flush(request.Addr, true)
	}
	return nil
}

func (rpc *RpcInterface) FindValue(request RpcPair, reply *ValueResult) error {
	*reply = rpc.node.FindValue(request.Info.(string))
	if rpc.node.addr != request.Addr {
		rpc.node.flush(request.Addr, true)
	}
	return nil
}

func (rpc *RpcInterface) Store(request RpcPair, reply *struct{}) error {
	rpc.node.Store(request.Info.(Pair))
	if rpc.node.addr != request.Addr {
		rpc.node.flush(request.Addr, true)
	}
	return nil
}

func (rpc *RpcInterface) Read(request RpcPair, reply *ReadResult) error {
	*reply = rpc.node.Read(request.Info.(string))
	if rpc.node.addr != request.Addr {
		rpc.node.flush(request.Addr, true)
	}
	return nil
}

func (rpc *RpcInterface) Ping(_ string, reply *struct{}) error {
	if !rpc.node.online {
		return errors.New("Ping error: offline")
	}
	return nil
}
