// This package implements a naive DHT protocol. (Actually, it is not distributed at all.)
// The performance and scalability of this protocol is terrible.
// You can use this as a reference to implement other protocols.
//
// In this naive protocol, the network is a complete graph, and each node stores all the key-value pairs.
// When a node joins the network, it will copy all the key-value pairs from another node.
// Any modification to the key-value pairs will be broadcasted to all the nodes.
// If any RPC call fails, we simply assume the target node is offline and remove it from the peer list.
package chord

import (
	rpc "dht/rpcNode"
	"errors"
	"fmt"
	"time"

	"math/big"
	"math/rand"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

// Note: The init() function will be executed when this package is imported.
// See https://golang.org/doc/effective_go.html#init for more details.
func init() {
	// You can use the logrus package to print pretty logs.
	// Here we set the log output to a file.
	f, _ := os.Create("dht-test.log")
	logrus.SetOutput(f)
}

// Pair is used to store a key-value pair.
// Note: It must be exported (i.e., Capitalized) so that it can be
// used as the argument type of RPC methods.
type Pair struct {
	Key   string
	Value string
}

type ValueReply struct {
	Value string
	Ok    bool
}

type TransferTarget struct {
	Pre    string
	Prepre string
}

const sucSize int = 10

type Node struct {
	addr   string // address and port number of the node, e.g., "localhost:1234"
	id     *big.Int
	online bool
	run    chan bool
	rpc.RpcNode

	data           map[string]string
	dataLock       sync.RWMutex
	dataBackup     map[string]string
	dataBackupLock sync.RWMutex

	finger      [m + 1]string
	fingerStart [m + 1]*big.Int
	fingerLock  sync.RWMutex

	predecessor string
	predeLock   sync.RWMutex

	successorList [sucSize]string
	sucLock       sync.RWMutex

	quitLock sync.RWMutex
}

// Initialize a node.
// Addr is the address and port number of the node, e.g., "localhost:1234".
func (node *Node) Init(addr string) {
	// logrus.Infof("Init %s", addr)
	node.addr = addr
	node.id = Hash(node.addr)
	node.online = false
	// fmt.Printf("%v\n", node.id)
	for i := 1; i <= m; i++ {
		node.fingerStart[i] = new(big.Int).Add(exp[i-1], node.id)
		if node.fingerStart[i].Cmp(exp[m]) >= 0 {
			node.fingerStart[i].Sub(node.fingerStart[i], exp[m])
		}
	}
}

func (node *Node) refresh() {
	// logrus.Infof("Refresh %s", node.addr)
	node.run = make(chan bool, 1)
	node.predeLock.Lock()
	node.predecessor = ""
	node.predeLock.Unlock()
	node.dataLock.Lock()
	node.data = make(map[string]string)
	node.dataLock.Unlock()
	node.dataBackupLock.Lock()
	node.dataBackup = make(map[string]string)
	node.dataBackupLock.Unlock()
	node.fingerLock.Lock()
	for i := 1; i <= m; i++ {
		node.finger[i] = node.addr
	}
	node.fingerLock.Unlock()
	node.sucLock.Lock()
	for i := 0; i < sucSize; i++ {
		node.successorList[i] = node.addr
	}
	node.sucLock.Unlock()
}

//
// DHT methods
//

func (node *Node) Run() {
	// logrus.Infof("Run %s", node.addr)
	node.refresh()
	node.online = true
	node.Register("Chord", node)
	go node.Serve(node.addr, node.run)
}

func (node *Node) Create() {
	// logrus.Infof("Create")
	node.predeLock.Lock()
	node.predecessor = node.addr
	node.predeLock.Unlock()
	<-node.run
	node.maintain()
}

func (node *Node) Join(addr string) bool {
	// logrus.Infof("Join %s to %s", node.addr, addr)
	// predecessor
	node.predeLock.Lock()
	node.predecessor = ""
	node.predeLock.Unlock()
	// successor (the successor of @node notify @node)
	var successor string
	<-node.run // run until Run() returns
	if err := node.RemoteCall(addr, "Chord.FindSuccessor", node.id, &successor); err != nil {
		// logrus.Error("Join err: cannot find successor")
		return false
	}
	node.sucLock.Lock()
	node.successorList[0] = successor
	node.sucLock.Unlock()
	// maintain the node
	node.maintain()
	return true
}

func (node *Node) Put(key string, value string) bool {
	// logrus.Infof("Put %s %s", key, value)
	var target string
	keyId := Hash(key)
	if err := node.FindSuccessor(keyId, &target); err != nil {
		// logrus.Error("find successor error when putting data: ", err)
		return false
	}
	var (
		reply bool
		data  []Pair
	)
	data = append(data, Pair{key, value})
	if err := node.RemoteCall(target, "Chord.PutAndReplicate", data, &reply); err != nil {
		// logrus.Error("call remotely \"PutAndReplicate()\" error when putting data: ", err)
		return false
	}
	return true
}

func (node *Node) Get(key string) (bool, string) {
	// logrus.Infof("Get %s", key)
	keyId := Hash(key)
	var successor string
	if err := node.FindSuccessor(keyId, &successor); err != nil {
		// logrus.Error("find successor error when getting data: ", err)
		return false, ""
	}
	var reply ValueReply
	if err := node.RemoteCall(successor, "Chord.GetPair", key, &reply); err != nil {
		// logrus.Error("call remotely \"GetPair()\" error when getting data: ", err)
		return false, ""
	}
	return reply.Ok, reply.Value
}

func (node *Node) Delete(key string) bool {
	// logrus.Infof("Delete %s", key)
	keyId := Hash(key)
	var successor string
	if err := node.FindSuccessor(keyId, &successor); err != nil {
		// logrus.Error("find successor error when deleting data: ", err)
		return false
	}
	var reply bool
	if err := node.RemoteCall(successor, "Chord.DeleteAllData", []string{key}, &reply); err != nil {
		// logrus.Error("call remotely \"DeleteAllData()\" error when deleting data: ", err)
		return false
	}
	return reply
}

func (node *Node) Quit() {
	// logrus.Infof("Quit %s", node.addr)
	if !node.online {
		// logrus.Error("already quitted")
		return
	}
	node.quitLock.Lock() // 阻塞stabilize
	node.online = false
	var list [sucSize]string
	node.updateSuccessorList()
	// node.updatePredecessor()
	node.sucLock.RLock()
	for i := 0; i < sucSize; i++ {
		list[i] = node.successorList[i]
	}
	node.sucLock.RUnlock()
	var predecessor string
	node.Predecessor("", &predecessor)
	// Inform all the nodes in the network that this node is quitting.
	var (
		wg            sync.WaitGroup
		predeQuitLock bool = false
		sucQuitLock   bool = false
	)
	defer func() {
		if predeQuitLock {
			node.RemoteCall(predecessor, "Chord.QuitUnlock", "", nil)
		}
		if sucQuitLock {
			node.RemoteCall(list[0], "Chord.QuitUnlock", "", nil)
		}
		node.StopServe()
		node.quitLock.Unlock()
	}()
	wg.Add(4)
	go func() {
		defer wg.Done()
		var (
			data []Pair
			keys []string
		)
		node.dataLock.RLock()
		for key, value := range node.data {
			data = append(data, Pair{key, value})
			keys = append(keys, key)
		}
		node.dataLock.RUnlock()
		var reply bool
		if err := node.RemoteCall(list[0], "Chord.PutAndReplicate", data, &reply); err != nil {
			// logrus.Error("call remotely \"PutAndReplicate()\" error when quitting: ", err)
			return
		}
		if err := node.RemoteCall(list[0], "Chord.DeleteBackup", keys, &reply); err != nil {
			// logrus.Errorf("[%s] call remotely \"DeleteBackup()\" error when qutting: %v", list[0], err)
			return
		}
	}()
	go func() {
		defer wg.Done()
		var dataBackup []Pair
		node.dataBackupLock.RLock()
		for key, value := range node.dataBackup {
			dataBackup = append(dataBackup, Pair{key, value})
		}
		node.dataBackupLock.RUnlock()
		var reply bool
		if err := node.RemoteCall(list[0], "Chord.ReplicateData", dataBackup, &reply); err != nil {
			// logrus.Errorf("[%s] call remotely \"ReplicateData()\" error when quitting: %v", list[0], err)
			return
		}
	}()
	go func() {
		defer wg.Done()
		if predecessor != "" && node.addr != predecessor {
			node.RemoteCall(predecessor, "Chord.QuitLock", "", nil)
			predeQuitLock = true
			node.RemoteCall(predecessor, "Chord.ChangeSuccessorList", list, nil)
		}
	}()
	go func() {
		defer wg.Done()
		if list[0] != node.addr {
			if list[0] != predecessor {
				node.RemoteCall(list[0], "Chord.QuitLock", "", nil)
				sucQuitLock = true
			}
			node.RemoteCall(list[0], "Chord.ChangePredecessor", predecessor, nil)
		}
	}()
	wg.Wait()
}

func (node *Node) ForceQuit() {
	// logrus.Infof("ForceQuit %s", node.addr)
	if !node.online {
		// logrus.Error("already force-quitted")
		return
	}
	node.online = false
	node.StopServe()
}

func (node *Node) maintain() {
	go func() {
		for {
			node.quitLock.Lock()
			if !node.online {
				node.quitLock.Unlock()
				break
			}
			node.stabilize()
			node.quitLock.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
	}()
	go func() {
		for node.online {
			node.fixFingers()
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

func (node *Node) stabilize() {
	// logrus.Infof("stabilize node %s", node.addr)
	// node.updateSuccessorList()
	// node.updatePredecessor()
	var successor string
	node.Successor("", &successor)
	successorId := Hash(successor)
	var x string
	if err := node.RemoteCall(successor, "Chord.Predecessor", "", &x); err != nil {
		// logrus.Error("call remotely \"Chord.Predecessor()\" error when stablilizing: ", err)
		return
	}
	if successor == node.addr || (x != "" && Belong(Hash(x), node.id, successorId, false, false)) {
		// logrus.Infof("change successor when stabilizing")
		node.sucLock.Lock()
		for i := sucSize - 1; i >= 1; i-- {
			node.successorList[i] = node.successorList[i-1]
		}
		node.successorList[0] = x
		node.sucLock.Unlock()
		var reply bool
		if err := node.RemoteCall(successor, "Chord.TransferData", TransferTarget{x, node.addr}, &reply); err != nil {
			// logrus.Error("transfer data error when stabilizing: ", err)
			return
		}
	}
	node.Successor("", &successor)
	if err := node.RemoteCall(successor, "Chord.Notify", node.addr, nil); err != nil {
		// logrus.Error("call remotely \"Chord.Notify()\" error when stabilizing: ", err)
		return
	}
}

func (node *Node) fixFingers() {
	i := rand.Intn(159) + 2
	// logrus.Infof("fix the %dth finger of node %s", i, node.addr)
	var reply string
	if err := node.FindSuccessor(node.fingerStart[i], &reply); err != nil {
		// logrus.Error("find successor error when fixing fingers: ", err)
		return
	}
	node.fingerLock.Lock()
	node.finger[i] = reply
	// logrus.Infof("finger[%v] is now %s", i, node.finger[i])
	node.fingerLock.Unlock()
}

func (node *Node) closestPrecedingFinger(id *big.Int) string {
	// logrus.Infof("Find the closest preceding finger to %v of node %s (ID: %v)", id, node.addr, node.id)
	for i := m; i > 1; i-- {
		node.fingerLock.RLock()
		fin := node.finger[i]
		node.fingerLock.RUnlock()
		if node.ping(fin) {
			fingerId := Hash(fin)
			if Belong(fingerId, node.id, id, false, false) {
				return fin
			}
		}
	}
	var successor string
	node.Successor("", &successor)
	if node.ping(successor) && Belong(Hash(successor), node.id, id, false, false) {
		return successor
	}
	return node.addr
}

func (node *Node) updateSuccessorList() {
	// logrus.Infof("update successor list of %s", node.addr)
	var list, nextList [sucSize]string
	node.SuccessorList("", &list)
	for i, addr := range list {
		if node.ping(addr) {
			if err := node.RemoteCall(addr, "Chord.SuccessorList", "", &nextList); err != nil {
				// logrus.Errorf("[%s] call remotely \"SuccessorList()\" error when updating successor list of %s: %v", addr, node.addr, err)
				continue
			}
			// fmt.Println(list)
			node.sucLock.Lock()
			node.successorList[0] = addr
			for j := 1; j < sucSize; j++ {
				node.successorList[j] = nextList[j-1]
			}
			node.sucLock.Unlock()
			if i == 1 {
				node.sucLock.RLock()
				successor := node.successorList[0]
				node.sucLock.RUnlock()
				if err := node.RemoteCall(successor, "Chord.BackupToMain", "", nil); err != nil {
					// logrus.Errorf("[%s] call remotely \"BackupToMain()\" error when updating successor list of %s: %v", successor, node.addr, err)
					return
				}
				var data []Pair
				node.dataLock.RLock()
				for key, value := range node.data {
					data = append(data, Pair{key, value})
				}
				node.dataLock.RUnlock()
				var reply bool
				if err := node.RemoteCall(successor, "Chord.ReplicateData", data, &reply); err != nil {
					// logrus.Errorf("[%s] call remotely \"ReplicateData()\" error when updating successor list of %s: %v", successor, node.addr, err)
					return
				}
			}
			return
		}
	}
}

func (node *Node) updatePredecessor() {
	node.predeLock.RLock()
	predecessor := node.predecessor
	node.predeLock.RUnlock()
	if predecessor != "" && !node.ping(predecessor) {
		node.predeLock.Lock()
		node.predecessor = ""
		node.predeLock.Unlock()
	}
}

//
// RPC Methods
//

// Note: The methods used for RPC must be exported (i.e., Capitalized),
// and must have two arguments, both exported (or builtin) types.
// The second argument must be a pointer.
// The return type must be error.
// In short, the signature of the method must be:
//   func (t *T) MethodName(argType T1, replyType *T2) error
// See https://golang.org/pkg/net/rpc/ for more details.

func (node *Node) QuitLock(_ string, _ *struct{}) error {
	node.quitLock.Lock()
	return nil
}

func (node *Node) QuitUnlock(_ string, _ *struct{}) error {
	node.quitLock.Unlock()
	return nil
}

func (node *Node) Notify(addr string, reply *struct{}) error {
	id := Hash(addr)
	// node.updatePredecessor()
	var predecessor string
	node.Predecessor("", &predecessor)
	predecessorId := Hash(predecessor)
	if predecessor == "" || Belong(id, predecessorId, node.id, false, false) {
		node.predeLock.Lock()
		node.predecessor = addr
		node.predeLock.Unlock()
	}
	return nil
}

// The difference between Successor() and FindSuccessor() is that
// Successor() returns successors with regard to nodes
// FindSuccessor() returns successors with regard to keys
// And the difference between Predecessor() and FindPredecessor() is the same as above
func (node *Node) Successor(_ string, reply *string) error {
	node.updateSuccessorList()
	node.sucLock.RLock()
	*reply = node.successorList[0]
	node.sucLock.RUnlock()
	return nil
}

func (node *Node) Predecessor(_ string, reply *string) error {
	node.updatePredecessor()
	node.predeLock.RLock()
	*reply = node.predecessor
	node.predeLock.RUnlock()
	return nil
}

func (node *Node) SuccessorList(_ string, reply *[sucSize]string) error {
	node.sucLock.RLock()
	*reply = node.successorList
	node.sucLock.RUnlock()
	return nil
}

func (node *Node) FindSuccessor(id *big.Int, reply *string) error {
	// logrus.Infof("Find successor of %v from node %s (ID: %v)", id, node.addr, node.id)
	if id.Cmp(node.id) == 0 {
		*reply = node.addr
		return nil
	}
	if err := node.FindPredecessor(id, reply); err != nil {
		// logrus.Error("find predecessor error when looking for successor: ", err)
		return err
	}
	if err := node.RemoteCall(*reply, "Chord.Successor", "", reply); err != nil {
		// logrus.Error("call remotely \"Chord.Successor()\" error when looking for successor: ", err)
		return err
	}
	return nil
}

func (node *Node) FindPredecessor(id *big.Int, reply *string) error {
	// logrus.Infof("Find predecessor of %v from node %s (ID: %v)", id, node.addr, node.id)
	*reply = node.addr
	var successor string
	node.Successor("", &successor)
	// fmt.Println(node.id.Cmp(Hash(successor)))
	if !Belong(id, node.id, Hash(successor), false, true) {
		var closest string = node.closestPrecedingFinger(id)
		// logrus.Infof("closest finger of node %s to ID %v : %s", node.addr, id, closest)
		if err := node.RemoteCall(closest, "Chord.FindPredecessor", id, reply); err != nil {
			// logrus.Error("call remotely \"Chord.FindPredecessor()\" error when looking for succesor: ", err)
			return err
		}
	}
	// fmt.Println(*reply)
	return nil
}

func (node *Node) ChangeSuccessorList(sucList [sucSize]string, _ *struct{}) error {
	node.sucLock.Lock()
	for i := 0; i < sucSize; i++ {
		node.successorList[i] = sucList[i]
	}
	node.sucLock.Unlock()
	return nil
}

func (node *Node) ChangePredecessor(prede string, _ *struct{}) error {
	node.predeLock.Lock()
	node.predecessor = prede
	node.predeLock.Unlock()
	return nil
}

func (node *Node) PutData(pair []Pair, reply *bool) error {
	*reply = true
	node.dataLock.Lock()
	for _, elem := range pair {
		node.data[elem.Key] = elem.Value
	}
	node.dataLock.Unlock()
	return nil
}

func (node *Node) ReplicateData(pair []Pair, reply *bool) error {
	*reply = true
	node.dataBackupLock.Lock()
	for _, elem := range pair {
		node.dataBackup[elem.Key] = elem.Value
	}
	node.dataBackupLock.Unlock()
	return nil
}

func (node *Node) PutAndReplicate(pair []Pair, reply *bool) error {
	var ret bool
	node.PutData(pair, reply)
	var successor string
	node.Successor("", &successor)
	if err := node.RemoteCall(successor, "Chord.ReplicateData", pair, &ret); err != nil {
		// logrus.Error("call remotely \"ReplicatePair()\" error: ", err)
		return err
	}
	*reply = *reply && ret
	return nil
}

func (node *Node) GetPair(key string, reply *ValueReply) error {
	node.dataLock.RLock()
	value, ok := node.data[key]
	node.dataLock.RUnlock()
	*reply = ValueReply{value, ok}
	return nil
}

func (node *Node) BackupToMain(_ string, reply *struct{}) error {
	var backup []Pair
	node.dataBackupLock.RLock()
	for key, value := range node.dataBackup {
		backup = append(backup, Pair{key, value})
	}
	node.dataBackupLock.RUnlock()
	node.dataBackupLock.Lock()
	node.dataBackup = make(map[string]string)
	node.dataBackupLock.Unlock()
	var ret bool
	return node.PutAndReplicate(backup, &ret)
}

func (node *Node) DeleteData(keys []string, reply *bool) error {
	*reply = true
	node.dataLock.Lock()
	for i := range keys {
		_, ok := node.data[keys[i]]
		if !ok {
			*reply = false
		} else {
			delete(node.data, keys[i])
		}
	}
	node.dataLock.Unlock()
	return nil
}

func (node *Node) DeleteBackup(keys []string, reply *bool) error {
	*reply = true
	node.dataBackupLock.Lock()
	for i := range keys {
		_, ok := node.dataBackup[keys[i]]
		if !ok {
			*reply = false
		} else {
			delete(node.dataBackup, keys[i])
		}
	}
	node.dataBackupLock.Unlock()
	return nil
}

func (node *Node) DeleteAllData(keys []string, reply *bool) error {
	var ret bool
	node.DeleteData(keys, reply)
	var successor string
	node.Successor("", &successor)
	if err := node.RemoteCall(successor, "Chord.DeleteBackup", keys, &ret); err != nil {
		// logrus.Errorf("[%s] delete backup data error when deleting all data of %s", successor, node.addr)
		return err
	}
	*reply = *reply && ret
	return nil
}

// transfer data to the predecessor after changing the successor
func (node *Node) TransferData(target TransferTarget, reply *bool) error {
	preId := Hash(target.Pre)
	prepreId := Hash(target.Prepre)
	var data, dataBackup []Pair
	var keys, keysBackup []string
	node.dataLock.RLock()
	for key, value := range node.data {
		if !Belong(Hash(key), preId, node.id, false, true) {
			data = append(data, Pair{key, value})
			keys = append(keys, key)
		}
	}
	node.dataLock.RUnlock()
	var deleteData, putData, replicateData, deleteBackup, replicateBackup bool
	node.DeleteAllData(keys, &deleteData)
	if err := node.RemoteCall(target.Pre, "Chord.PutData", data, &putData); err != nil {
		// logrus.Error("call remotely \"PutData()\" error when transferring data: ", err)
		return err
	}
	node.ReplicateData(data, &replicateData)
	node.dataBackupLock.RLock()
	for key, value := range node.dataBackup {
		if !Belong(Hash(key), prepreId, preId, false, true) {
			dataBackup = append(dataBackup, Pair{key, value})
			keysBackup = append(keysBackup, key)
		}
	}
	node.dataBackupLock.RUnlock()
	node.DeleteBackup(keysBackup, &deleteBackup)
	if err := node.RemoteCall(target.Pre, "Chord.ReplicateData", dataBackup, &replicateBackup); err != nil {
		// logrus.Errorf("[%s] call remotely \"ReplicateData()\" error when transferring data: %v", target.Pre, err)
		return err
	}
	return nil
}

func (node *Node) ping(addr string) bool {
	if err := node.RemoteCall(addr, "Chord.Ping", "", nil); err != nil {
		err = node.RemoteCall(addr, "Chord.Ping", "", nil)
		return err == nil
	}
	return true
}

func (node *Node) Ping(_ string, _ *struct{}) error {
	if node.online {
		return nil
	}
	return errors.New("Offline")
}

func (node *Node) Traverse(str string, reply *struct{}) error {
	fmt.Println(node.addr)
	if node.successorList[0] == str {
		fmt.Println("------------------------------")
		return nil
	}
	node.RemoteCall(node.successorList[0], "Chord.Traverse", str, nil)
	return nil
}
