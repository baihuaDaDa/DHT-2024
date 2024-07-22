package application

import (
	"dht/kademlia"
)

/*
 * In this file, you need to implement the "NewNode" function.
 * This function should create a new DHT node and return it.
 * You can use the "naive.Node" struct as a reference to implement your own struct.
 */

func NewNode(addr string) dhtNode {
	// Todo: create a node and then return it.
	node := new(kademlia.Node)
	node.Init(addr)
	return node
}
