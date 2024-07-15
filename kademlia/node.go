package kademlia

import (
	rpc "dht/rpcNode"

	"math/big"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

func init() {
	f, _ := os.Create("dht-test.log")
	logrus.SetOutput(f)
}

const (
	k     int = 20
	alpha int = 3
)

type ValueResult struct {
	NodeList []string
	Value    string
	Found    bool
}

type ReadResult struct {
	Value string
	Ok    bool
}

type Pair struct {
	Key   string
	Value string
}

type Node struct {
	addr   string
	id     *big.Int
	online bool
	run    chan bool
	rpc.RpcNode
	data     Data
	kBuckets [m + 1]Bucket
}

func (node *Node) Init(addr string) {
	logrus.Infof("Init %s", addr)
	node.addr = addr
	node.id = Hash(node.addr)
	node.online = false
}

func (node *Node) reset() {
	logrus.Infof("Refresh %s", node.addr)
	node.run = make(chan bool, 1)
	node.data.init()
	for i := range node.kBuckets {
		node.kBuckets[i].init(node)
	}
}

//
// DHT methods
//

func (node *Node) Run() {
	logrus.Infof("Run %s", node.addr)
	node.reset()
	node.online = true
	node.Register("Kademlia", &RpcInterface{node})
	go node.Serve(node.addr, node.run)
}

func (node *Node) Create() {
	logrus.Infof("Create")
	<-node.run
	// node.maintain()
}

func (node *Node) Join(addr string) bool {
	logrus.Infof("Join %s to %s", node.addr, addr)
	<-node.run
	i := Locate(node.id, Hash(addr))
	node.kBuckets[i].pushBack(addr)
	node.nodeLookup(node.id)
	// node.maintain()
	return true
}

// 如果直接找到了key不能直接返回，而是需要修改所有key对应的value的数据
func (node *Node) Put(key string, value string) bool {
	logrus.Infof("Put %s %s", key, value)
	return node.publishData(Pair{key, value})
}

func (node *Node) Get(key string) (bool, string) {
	logrus.Infof("Get %s", key)
	result := node.valueLookup(key)
	if result.Found {
		return true, result.Value
	}
	return false, ""
}

func (node *Node) Delete(key string) bool {
	logrus.Infof("Delete %s but nothing happens", key)
	return true
}

func (node *Node) Quit() {
	logrus.Infof("Quit %s", node.addr)
	if !node.online {
		logrus.Error("already quitted")
		return
	}
	node.online = false
	node.republish(node.data.getAll())
	node.StopServe()
}

func (node *Node) ForceQuit() {
	logrus.Infof("ForceQuit %s", node.addr)
	if !node.online {
		logrus.Error("already force-quitted")
		return
	}
	node.online = false
	node.StopServe()
}

func (node *Node) Traverse(str string, reply *struct{}) error {
	return nil
}

// RPC
// 不能返回requester
func (node *Node) FindNode(id *big.Int) (nodeList []string) {
	i := Locate(node.id, id)
	var bucket []string
	if i == -1 {
		nodeList = append(nodeList, node.addr)
	} else {
		bucket = node.kBuckets[i].getAll()
		nodeList = append(nodeList, bucket...)
	}
	if len(nodeList) == k {
		return nodeList
	}
	for j := i - 1; j >= 0; j-- {
		bucket = node.kBuckets[j].getAll()
		for l := range bucket {
			nodeList = append(nodeList, bucket[l])
			if len(nodeList) == k {
				return nodeList
			}
		}
	}
	for j := i + 1; j < m; j++ {
		bucket = node.kBuckets[j].getAll()
		for l := range bucket {
			nodeList = append(nodeList, bucket[l])
			if len(nodeList) == k {
				return nodeList
			}
		}
	}
	if i != -1 {
		nodeList = append(nodeList, node.addr)
	}
	return nodeList
}

func (node *Node) nodeLookup(id *big.Int) (nodeList []string) {
	var (
		set         Set
		closestNode string
	)
	set.init()
	list := node.FindNode(id)
	set.insert(node.addr)
	for i := range list {
		set.insert(list[i])
	}
	for {
		closestNode = set.getFront()
		callList := set.getCallList()
		node.findNodeList(&set, callList, id)
		if set.getFront() == closestNode {
			callList = set.getCallList()
			nodeList = node.findNodeList(&set, callList, id)
			break
		}
	}
	return nodeList
}

func (node *Node) findNodeList(set *Set, callList []string, id *big.Int) (nodeList []string) {
	var wg sync.WaitGroup
	for i := range callList {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			var subNodeList []string
			if err := node.RemoteCall(addr, "Kademlia.FindNode", RpcPair{node.addr, id}, &subNodeList); err != nil {
				set.delete(addr)
				node.flush(addr, false)
				return
			}
			node.flush(addr, true)
			for j := range subNodeList {
				if set.insert(subNodeList[j]) {
					nodeList = append(nodeList, subNodeList[j])
				}
			}
		}(callList[i])
	}
	wg.Wait()
	return nodeList[:k]
}

// RPC
func (node *Node) FindValue(key string) ValueResult {
	value, ok := node.data.get(key)
	if ok {
		return ValueResult{[]string{}, value, true}
	}
	return ValueResult{node.FindNode(Hash(key)), "", false}
}

// RPC
func (node *Node) Store(pair Pair) {
	node.data.put(pair)
}

// RPC
func (node *Node) Read(key string) ReadResult {
	value, ok := node.data.get(key)
	return ReadResult{value, ok}
}

func (node *Node) valueLookup(key string) ValueResult {
	var (
		set         Set
		closestNode string
	)
	set.init()
	result := node.FindValue(key)
	if result.Found {
		return result
	}
	set.insert(node.addr)
	for i := range result.NodeList {
		set.insert(result.NodeList[i])
	}
	for {
		closestNode = set.getFront()
		callList := set.getCallList()
		nodeList, value, found := node.findValueList(&set, callList, key)
		if found {
			return ValueResult{nodeList, value, found}
		}
		if closestNode == set.getFront() {
			callList = set.getCallList()
			nodeList, value, found = node.findValueList(&set, callList, key)
			return ValueResult{nodeList, value, found}
		}
	}
}

func (node *Node) findValueList(set *Set, callList []string, key string) (nodeList []string, value string, found bool) {
	found = false
	for i := range callList {
		var result ValueResult
		if err := node.RemoteCall(callList[i], "Kademlia.FindValue", RpcPair{node.addr, key}, &result); err != nil {
			set.delete(callList[i])
			node.flush(callList[i], false)
			continue
		}
		node.flush(callList[i], true)
		if result.Found {
			value = result.Value
			found = true
			return nodeList, value, found
		}
		for j := range result.NodeList {
			if set.insert(result.NodeList[j]) {
				nodeList = append(nodeList, result.NodeList[j])
			}
		}
	}
	return nodeList[:k], value, found
}

func (node *Node) publishData(pair Pair) bool {
	nodeList := node.nodeLookup(Hash(pair.Key))
	flag := false
	var wg sync.WaitGroup
	for i := range nodeList {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			if addr == node.addr {
				node.Store(pair)
				flag = true
			} else {
				if err := node.RemoteCall(addr, "Kademlia.Store", RpcPair{node.addr, pair}, nil); err != nil {
					node.flush(addr, false)
				} else {
					node.flush(addr, true)
					flag = true
				}
			}
		}(nodeList[i])
	}
	wg.Wait()
	return flag
}

func (node *Node) republish(republishList []Pair) {
	var wg sync.WaitGroup
	for _, pair := range republishList {
		wg.Add(1)
		go func(data Pair) {
			defer wg.Done()
			node.publishData(data)
		}(pair)
	}
	wg.Wait()
}

func (node *Node) refresh() {
}

func (node *Node) expire() {
	node.data.expire()
}

func (node *Node) flush(addr string, online bool) {
	i := Locate(node.id, Hash(addr))
	if i != -1 {
		node.kBuckets[i].flush(addr, online)
	}
}

func (node *Node) maintain() {
	go func() {
		for node.online {
			node.refresh()
		}
	}()
	go func() {
		for node.online {
			node.republish(node.data.getRepublishList())
		}
	}()
	go func() {
		for node.online {
			node.expire()
		}
	}()
}

func (node *Node) ping(addr string) bool {
	err := node.RemoteCall(addr, "Kademlia.Ping", "", nil)
	return err == nil
}
