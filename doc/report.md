# Project Report: Implementation of Chord and Kademlia DHT Protocols

## Introduction

In this project, I have implemented two distinct distributed hash table (DHT) protocols: Chord and Kademlia. Both protocols aim to provide efficient, scalable, and fault-tolerant systems for storing and retrieving (key, value) pairs across a distributed network of nodes. This report details the architecture and features of my implementations, alongside several significant problems I met when debugging.

## Chord Protocol

### System Architecture

The Chord protocol implementation revolves around a consistent hashing ring where each node and data item is assigned a unique identifier using a SHA-1 hash function. The primary functionalities implemented include:

- **Node Initialization and Joining**: New nodes initialize their state and join the ring by finding their position based on their hashed ID. Their position is exactly behind the succeeding node of their ID, which means the node is the first node not located before the ID in the direction of the ring.  Each node maintains information about its successor and predecessor to facilitate data lookup and retrieval. The nodes also maintains their finger tables to accelerate looking up data.

>Here is some key functions indicating how the protocol locating nodes and keys.
```go
// FindSuccessor finds the successor node of a given ID in the Chord distributed system.
// It first checks if the current node is the successor, and if so, directly returns its address.
// If not, it finds the predecessor node and then requests the predecessor for its successor.
// Parameters:
//   id - The ID for which to find the successor.
//   reply - A pointer to a string where the address of the successor will be stored.
// Return value:
//   Returns an error if finding the predecessor or requesting the successor from the predecessor fails.
func (node *Node) FindSuccessor(id *big.Int, reply *string) error {
	// Check if the current node is the successor
	if id.Cmp(node.id) == 0 {
		*reply = node.addr
		return nil
	}
	// Find the predecessor node
	if err := node.FindPredecessor(id, reply); err != nil {
		return err
	}
	// Request the successor from the predecessor
	if err := node.RemoteCall(*reply, "Chord.Successor", "", reply); err != nil {
		return err
	}
	return nil
}

// FindPredecessor finds the predecessor node of a given ID in the Chord distributed system.
// It first assumes the current node is the predecessor and prepares to return its address.
// If the given ID does not belong to the sub-interval managed by the current node and its successor,
// it finds the closest preceding finger node and requests that node to find the predecessor.
// Parameters:
//   id - The ID for which to find the predecessor.
//   reply - A pointer to a string where the address of the predecessor will be stored.
// Return value:
//   Returns an error if requesting the predecessor from the closest preceding finger node fails.
func (node *Node) FindPredecessor(id *big.Int, reply *string) error {
	// Set the current node's address as the initial predecessor
	*reply = node.addr
	var successor string
	node.Successor("", &successor)
	// Check if the given ID belongs to the sub-interval managed by the current node and its successor
	if !Belong(id, node.id, Hash(successor), false, true) {
		// Find the closest preceding finger node
		var closest string = node.closestPrecedingFinger(id)
		// Request the closest preceding finger node to find the predecessor
		if err := node.RemoteCall(closest, "Chord.FindPredecessor", id, reply); err != nil {
			return err
		}
	}
	return nil
}

// closestPrecedingFinger finds the closest preceding node in the finger table that precedes the given ID.
// This method is crucial for efficient routing and data lookup in distributed systems.
// Parameters:
//   id: A big integer representing the ID for which the closest preceding node is sought.
// Returns:
//   A string representing the address of the closest preceding node found.
func (node *Node) closestPrecedingFinger(id *big.Int) string {
    // Iterate backwards through the finger table entries from 'm' down to 1.
    // logrus.Infof("Find the closest preceding finger to %v of node %s (ID: %v)", id, node.addr, node.id)
    for i := m; i > 1; i-- {
        // Acquire read lock for concurrent-safe access to the finger table.
        node.fingerLock.RLock()
        fin := node.finger[i]
        // Release read lock.
        node.fingerLock.RUnlock()
        // Check if the finger node is reachable.
        if node.ping(fin) {
            // Calculate the hash value of the finger node.
            fingerId := Hash(fin)
            // Determine if the finger node falls within the range preceding the target ID.
            if Belong(fingerId, node.id, id, false, false) {
                return fin
            }
        }
    }
    // If no suitable finger node is found, attempt to retrieve the successor node.
    var successor string
    node.Successor("", &successor)
    // Check if the successor node is reachable and falls within the range preceding the target ID.
    if node.ping(successor) && Belong(Hash(successor), node.id, id, false, false) {
        return successor
    }
    // If all checks fail, return the current node's own address as the result.
    return node.addr
}
```

- **Data Management**: Each node holds data items for which it is responsible according to its position on the hash ring. Data insertion, retrieval, and deletion are managed locally but involve communication across the network to ensure consistency. Also, each node replicates the backup of the data for which the proceding node is responsible at the aim of fault tolerance.

- **Stabilization**: Regular stabilization processes are executed to manage node joins and departures, ensuring the ring maintains its integrity and data remains accessible.

- **Fault Tolerance**: Both successors and data of each node are backed up when they are saved in the network to prevent possible damages of the ring structure or database due to accidentally force quit.

>Here is some key functions of the protocol featuring maintaining the consistency of data and the ring structure.
```go
// updateSuccessorList updates the successor list of the current node by remote procedure calls.
// This method first retrieves the current node's successor list and then checks the reachability of each node in the list.
// If a node is reachable, it fetches that node's successor list and updates its own successor list accordingly.
// Additionally, if the first reachable node in the successor list is found, the current node will back up its data to this node and replicate the data to the successors.
func (node *Node) updateSuccessorList() {
    // Initialize two string arrays to store the current node's successor list and the next node's successor list
    var list, nextList [sucSize]string
    // Call SuccessorList method to get the current node's successor list
    node.SuccessorList("", &list)
    // Iterate over the current node's successor list
    for i, addr := range list {
        // Check if the current successor node is reachable
        if node.ping(addr) {
            // If reachable, attempt to remotely call "SuccessorList" on the successor node
            if err := node.RemoteCall(addr, "Chord.SuccessorList", "", &nextList); err != nil {
                // Log an error message if the remote call fails
                // continue to the next iteration if an error occurs
                continue
            }
            // Lock the successor list for write access to update the successor list
            node.sucLock.Lock()
            // Update the first element of the successor list with the current address
            node.successorList[0] = addr
            // Update the rest of the successor list with the next list
            for j := 1; j < sucSize; j++ {
                node.successorList[j] = nextList[j-1]
            }
            // Unlock the successor list after updating
            node.sucLock.Unlock()
            // If the current index is 1, perform additional operations for data backup and replication
            if i == 1 {
                // Read-lock the successor list to retrieve the primary successor
                node.sucLock.RLock()
                // Get the primary successor from the list
                successor := node.successorList[0]
                // Release the read-lock on the successor list
                node.sucLock.RUnlock()
                // Attempt to remotely call "BackupToMain" on the primary successor
                if err := node.RemoteCall(successor, "Chord.BackupToMain", "", nil); err != nil {
                    // Log an error message if the remote call fails
                    return
                }
                // Create a slice to hold the data pairs for replication
                var data []Pair
                // Read-lock the data for safe iteration
                node.dataLock.RLock()
                // Iterate over the local data map and append the key-value pairs to the data slice
                for key, value := range node.data {
                    data = append(data, Pair{key, value})
                }
                // Release the read-lock on the data
                node.dataLock.RUnlock()
                // Variable to hold the response from the remote call
                var reply bool
                // Attempt to remotely call "ReplicateData" on the primary successor with the data slice
                if err := node.RemoteCall(successor, "Chord.ReplicateData", data, &reply); err != nil {
                    // Log an error message if the remote call fails
                    return
                }
            }
            // Exit the function once a reachable node has been processed
            return
        }
    }
}

// Quit allows a node to safely exit the Chord distributed system.
// It ensures that before exiting, the node updates its successor and predecessor information,
// and transfers its data and backed up data to the successor, maintaining the system's operational integrity.
// Remember to replicate the data to be transferred in the successor of the quitting node's successor.
func (node *Node) Quit() {
    // If the node is already offline, simply return.
    if !node.online {
        return
    }
    // Lock to prevent other operations from interfering during the exit process.
    node.quitLock.Lock() // Block stabilization
    node.online = false
    var list [sucSize]string
    // update successor list before using it.
    node.updateSuccessorList()
    node.sucLock.RLock()
    for i := 0; i < sucSize; i++ {
        list[i] = node.successorList[i]
    }
    node.sucLock.RUnlock()
    var predecessor string
    node.Predecessor("", &predecessor) // This method already includes updating predecessor.
    // Inform all nodes in the network that this node is quitting.
    var (
        wg            sync.WaitGroup
        predeQuitLock bool = false
        sucQuitLock   bool = false
    )
    // ensure the block lock is unlocked and the RPC service is stopped
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
        // transfer main data
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
            return
        }
        if err := node.RemoteCall(list[0], "Chord.DeleteBackup", keys, &reply); err != nil {
            return
        }
    }()
    go func() {
        // transfer backed up data
        defer wg.Done()
        var dataBackup []Pair
        node.dataBackupLock.RLock()
        for key, value := range node.dataBackup {
            dataBackup = append(dataBackup, Pair{key, value})
        }
        node.dataBackupLock.RUnlock()
        var reply bool
        if err := node.RemoteCall(list[0], "Chord.ReplicateData", dataBackup, &reply); err != nil {
            return
        }
    }()
    go func() {
        // update successor list of the predecessor
        defer wg.Done()
        if predecessor != "" && node.addr != predecessor {
            node.RemoteCall(predecessor, "Chord.QuitLock", "", nil)
            predeQuitLock = true
            node.RemoteCall(predecessor, "Chord.ChangeSuccessorList", list, nil)
        }
    }()
    go func() {
        // update predecessor of the successor
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

// Stabilize is a method that helps a node to stabilize its pointers,
// ensuring that the node's successor list is correctly updated and maintained.
// It does this by communicating with the current successor and predecessor nodes,
// and transferring any keys that may have been misassigned due to changes in the network topology.

// This method does not take any parameters as it uses the internal state of the Node struct.
// There are no return values from this method.
// This method is called regularly (every 50 milliseconds).

func (node *Node) stabilize() {
    var successor string
    node.Successor("", &successor)
    successorId := Hash(successor)

    var x string
    if err := node.RemoteCall(successor, "Chord.Predecessor", "", &x); err != nil {
        // Error handling for remote call failure.
        return
    }

    // Checks if the current node should update its successor.
    if successor == node.addr || (x != "" && Belong(Hash(x), node.id, successorId, false, false)) {
        node.sucLock.Lock()
        // Shifts the successor list to make room for the new successor.
        for i := sucSize - 1; i >= 1; i-- {
            node.successorList[i] = node.successorList[i-1]
        }
        // Sets the new successor at the head of the list.
        node.successorList[0] = x
        node.sucLock.Unlock()

        var reply bool
        if err := node.RemoteCall(successor, "Chord.TransferData", TransferTarget{x, node.addr}, &reply); err != nil {
            // Error handling for data transfer failure.
            return
        }
    }

    // Notifies the successor about the change in the predecessor.
    node.Successor("", &successor)
    if err := node.RemoteCall(successor, "Chord.Notify", node.addr, nil); err != nil {
        // Error handling for remote notification failure.
        return
    }
}

// Notify updates the predecessor information of the current node if necessary.
// This function is called by other nodes to inform this node that it might be the predecessor.
// Parameters:
//   addr: The address of the node which is potentially the new predecessor.
//   reply: An empty struct pointer used as a placeholder for RPC conventions.
// Returns:
//   error: Always returns nil as there's no operation that can fail in this function.
func (node *Node) Notify(addr string, reply *struct{}) error {
	id := Hash(addr)
	var predecessor string
	node.Predecessor("", &predecessor)
	predecessorId := Hash(predecessor)

	// If the node has no predecessor or the given node falls within the range between the current predecessor and itself,
	// update the predecessor to the given node's address.
	if predecessor == "" || Belong(id, predecessorId, node.id, false, false) {
		node.predeLock.Lock()
		node.predecessor = addr
		node.predeLock.Unlock()
	}
	return nil
}

// TransferData transfers a portion of the node's data to a specified target node,
// facilitating data migration or replication. This method is primarily used during
// node departure or addition to the network.
// Parameters:
//   target - Information about the target node to which data will be transferred.
//   reply - A pointer to a boolean value to receive the operation result.
// Returns:
//   error - If an error occurs during the operation, it returns the error; otherwise, returns nil.
func (node *Node) TransferData(target TransferTarget, reply *bool) error {
    // Calculate the hash values of the target node's predecessor and predecessor's predecessor
    preId := Hash(target.Pre)
    prepreId := Hash(target.Prepre)

    // Initialize slices to store data to be transferred and backup data
    var data, dataBackup []Pair
    var keys, keysBackup []string

    // Lock read access to iterate over the current node's data to prevent data race conditions
    node.dataLock.RLock()
    for key, value := range node.data {
        // If the data does not belong to the target node's predecessor, add it to the list of data to transfer
        if !Belong(Hash(key), preId, node.id, false, true) {
            data = append(data, Pair{key, value})
            keys = append(keys, key)
        }
    }
    node.dataLock.RUnlock()

    // Flags indicating whether local data needs to be deleted, remote PutData call, data replication to backup
    var deleteData, putData, replicateData, deleteBackup, replicateBackup bool

    // Delete local data that is no longer needed
    node.DeleteAllData(keys, &deleteData)

    // Remotely call the target node's predecessor to place the data to be transferred
    if err := node.RemoteCall(target.Pre, "Chord.PutData", data, &putData); err != nil {
        // logrus.Error("call remotely \"PutData()\" error when transferring data: ", err)
        return err
    }

    // Replicate the data to be transferred locally for potential recovery operations
    node.ReplicateData(data, &replicateData)

    // Lock read access to iterate over the current node's backup data to prevent data race conditions
    node.dataBackupLock.RLock()
    for key, value := range node.dataBackup {
        // If the backup data does not belong to the target node's predecessor's predecessor, add it to the list of backup data to transfer
        if !Belong(Hash(key), prepreId, preId, false, true) {
            dataBackup = append(dataBackup, Pair{key, value})
            keysBackup = append(keysBackup, key)
        }
    }
    node.dataBackupLock.RUnlock()

    // Delete backup data that is no longer needed
    node.DeleteBackup(keysBackup, &deleteBackup)

    // Remotely call the target node's predecessor to replicate the backup data to be transferred
    if err := node.RemoteCall(target.Pre, "Chord.ReplicateData", dataBackup, &replicateBackup); err != nil {
        // logrus.Errorf("[%s] call remotely \"ReplicateData()\" error when transferring data: %v", target.Pre, err)
        return err
    }

    // Operation successful, return nil
    return nil
}

// fixFingers repairs the pointers of a node to ensure valid pointers in the P2P network.
// This method randomly selects a pointer for repair to maintain network stability.
// It attempts to find the rightful successor that the selected pointer should point to and updates it accordingly.
func (node *Node) fixFingers() {
    // Randomly select an index of the finger to be fixed, ranging from 2 to 159.
    // The range is determined by the number of fingers per node and the network's distribution characteristics.
    i := rand.Intn(159) + 2

    // Attempt to find the successor that the current finger index should point to.
    // If the search fails, it might be due to network issues or the absence of a successor, in which case the function exits.
    var reply string
    if err := node.FindSuccessor(node.fingerStart[i], &reply); err != nil {
        // Error logging would occur here.
        return
    }

    // Lock to ensure data consistency while updating the finger direction.
    node.fingerLock.Lock()
    // Update the selected finger index to point to the found successor.
    node.finger[i] = reply
    // Logging the updated finger information would happen here.
    // Unlock to allow other operations to access or modify the fingers.
    node.fingerLock.Unlock()
}
```

### Problems When Debugging

- **Dialing: i/o timeout**: Since I haven't implemented a connection pool, every time the RPC client calls a function from the server remotely it needs to dial and connect the server again. As a result, the number of time_wait states goes beyond the upper limit of 65535 and error occurs when new TCP connections are being established.

- **Deadlock**: When the block between a lock returns with an error, the lock may have not been unlocked, leading to a dead lock.

- **Fail when stopping RPC service**: It is the same as the above problem. When the method `Quit` returns with an error, the RPC service may have not been stopped.
>Solved with `defer`

- **Uncommon `Belong` function**: The determination of whether an element belongs to an interval in the protocol is not exactly the same as our mathematical common sense.
>Solved with
```go
// Belong determines if a target number falls within a specified range.
// The parameters target, left, and right represent the target number and the range boundaries, respectively.
// The parameters leftClose and rightClose indicate whether the left and right boundaries are inclusive, respectively.
// The return value indicates whether the target number is within the range.
func Belong(target, left, right *big.Int, leftClose, rightClose bool) bool {
    // Compare the relationships between target, left, and right to determine the position of target relative to the range boundaries.
	lrCmp, tlCmp, trCmp := left.Cmp(right), target.Cmp(left), target.Cmp(right)
    
    // If left < right, the range is valid; determine if target is within the range.
	if lrCmp > 0 {
        // If target is to the left of left or to the right of right, or if it is equal to left or right and the corresponding boundary is inclusive, then target is within the range.
		// fmt.Print(2)
		return (tlCmp > 0 || trCmp < 0) || (tlCmp == 0 && leftClose) || (trCmp == 0 && rightClose)
	} else if lrCmp < 0 {
        // If left > right, the range is invalid, and target is not within the range.
		// fmt.Print(3)
		return (target.Cmp(left) > 0 && target.Cmp(right) < 0) || (tlCmp == 0 && leftClose) || (trCmp == 0 && rightClose)
	} else {
        // If left == right, the range is a single point; determine if target is that point or if it is equal to the boundary and the boundary is inclusive.
		return (tlCmp == 0 && (leftClose || rightClose)) || tlCmp != 0 // 注意这里如果左右端点重合，假如target不等于端点则always true
	}
}
```

- **Transferring data**: Remember to transfer the data backed up in the node.
>Solution: already demonstrated above.

## Kademlia Protocol

### System Architecture

Kademlia uses a different approach based on a binary XOR metric for distance between IDs, and organizes nodes into a k-bucket system for efficient routing:

- **Node Initialization and Routing**: Each node initializes its k-buckets, which store information about other nodes in the system sorted by their distance. The routing table dynamically adjusts as nodes join and leave.

>Here is key functions featuring finding target ID and maintaining the routing table.
```go
// FindNode searches for nodes closest to the given id in the node's routing table.
// It returns a list of node IDs that are closest to the target id.
// RPC
// 不能返回requester
func (node *Node) FindNode(id *big.Int) (nodeList []string) {
    // Determine the position of the target id in the routing table
    i := Locate(node.id, id)
    
    // Ensure log information is recorded before returning
    defer func() {
        log := "FindNode result [" + node.addr + "]: "
        for j := range nodeList {
            log = log + nodeList[j] + "||"
        }
        // logrus.Info(log)
    }()
    
    var bucket []string
    // If the target id is not in any bucket, directly return the node's address
    if i == -1 {
        // nodeList = append(nodeList, node.addr)
    } else {
        // Get all node IDs from the bucket where the target id is located
        bucket = node.kBuckets[i].getAll()
        nodeList = append(nodeList, bucket...)
    }
    
    // If the returned node list is already of the required size, return immediately
    if len(nodeList) == k {
        return nodeList
    }
    
    // Traverse the buckets before and after the target id to find additional closest nodes
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
    
    // If i != -1 {
    // 	nodeList = append(nodeList, node.addr)
    // }
    return nodeList
}

// nodeLookup initiates a search for nodes closest to the target id, starting from the current node.
// It returns a list of node IDs that are closest to the target id.
func (node *Node) nodeLookup(id *big.Int) (nodeList []string) {
    var (
        set         Set
        closestNode string
    )
    set.init(node.id)
    list := node.FindNode(id)
    set.mark(node.addr)
    for i := range list {
        set.insert(list[i])
    }
    for {
        closestNode = set.getFront()
        callList := set.getCallList()
        log := "getCallList result [" + node.addr + "]: "
        for i := range callList {
            log = log + callList[i] + "||"
        }
        // logrus.Info(log)
        node.findNodeList(&set, callList, id)
        if set.empty() || set.getFront() == closestNode {
            callList = set.getCallList()
            node.findNodeList(&set, callList, id)
            nodeList = set.getNodeList()
            break
        }
    }
    log := "nodeLookup result [" + node.addr + "]: "
    for i := range nodeList {
        log = log + nodeList[i] + "||"
    }
    // logrus.Info(log)
    return nodeList
}

// findNodeList asynchronously requests the list of nodes closest to the target id from the nodes in callList.
// It updates the set with the returned node IDs and removes any nodes that do not respond.
func (node *Node) findNodeList(set *Set, callList []string, id *big.Int) {
    var wg sync.WaitGroup
    for i := range callList {
        wg.Add(1)
        go func(addr string) {
            defer wg.Done()
            var subNodeList []string
            // Asynchronously call the FindNode method of the remote node
            if err := node.RemoteCall(addr, "Kademlia.FindNode", RpcPair{node.addr, id}, &subNodeList); err != nil {
                set.delete(addr)
                node.flush(addr, false)
                return
            }
            node.flush(addr, true)
            for j := range subNodeList {
                set.insert(subNodeList[j])
            }
        }(callList[i])
    }
    wg.Wait()
}

// flush handles the addressing and maintenance of elements in the bucket based on the online status.
// addr is the address to be processed, online indicates whether the node is currently online.
func (bucket *Bucket) flush(addr string, online bool) {
    // If the node is online
    if online {
        // Attempt to find the position of the address
        pos := bucket.find(addr)
        // If the address is found
        if pos != nil {
            // Move the found element to the end of the list
            bucket.shiftToBack()
        } else {
            // If the bucket is not full
            if bucket.size() < k {
                // Add the address to the end of the list
                bucket.pushBack(addr)
            } else {
                // If the node at the front of the bucket is alive
                if bucket.node.ping(bucket.front()) {
                    // Move the front element to the end of the list
                    bucket.shiftToBack()
                } else {
                    // Remove the front element and add the new address to the end
                    bucket.popFront()
                    bucket.pushBack(addr)
                }
            }
        }
    } else {
        // If the node is offline, remove the address from the bucket
        bucket.delete(addr)
    }
}
```

- **Data Management**: Similar to Chord, data management commands (put, get, delete) are implemented, leveraging the k-bucket system for efficient routing of requests.

>Here is the method publishing data in `func (node *Node) Put(key string) bool`. It calls `nodeLookup` instead of `valueLookup` because it needs to update the values when the keys already exists.
```go
// publishData attempts to store a data pair in the network of nodes. If the node is responsible for storing the data, it stores the data locally;
// otherwise, it tries to send the storage request to other nodes. Returns true if the data is successfully stored locally or sent to other nodes for storage.
// Parameters:
//   pair - The data pair to be stored, consisting of a key and its corresponding value.
// Return value:
//   bool - Returns true if the data is successfully stored; otherwise, returns false.
func (node *Node) publishData(pair Pair) bool {
    // Look up the list of nodes responsible for storing the data based on the hash of the key.
    nodeList := node.nodeLookup(Hash(pair.Key))
    flag := false
    var wg sync.WaitGroup
    // Iterate through the list of nodes, asynchronously attempting to store the data.
    for i := range nodeList {
        wg.Add(1)
        go func(addr string) {
            defer wg.Done()
            // If the current node is responsible for storing the data, store the data locally.
            if addr == node.addr {
                node.Store(pair)
                flag = true
            } else {
                // If it's not the responsible node, try to send the storage request to other nodes.
                if err := node.RemoteCall(addr, "Kademlia.Store", RpcPair{node.addr, pair}, nil); err != nil {
                    // If the remote call fails, mark the connection to this node for flushing.
                    node.flush(addr, false)
                } else {
                    // If the remote call succeeds, mark the connection to this node for flushing and update the flag indicating successful storage.
                    node.flush(addr, true)
                    flag = true
                }
            }
        }(nodeList[i])
    }
    // Wait for all asynchronous storage attempts to complete.
    wg.Wait()
    // Return whether the data was successfully stored.
    return flag
}
```

- **Network Refresh, Republish and Expire**: Nodes periodically refresh their k-buckets, republish valid stored data and abandon expired data to ensure high availability and fault tolerance.

### Problems When Debugging

- **Getting data**: Remember to scan the node list returned by `valueLookup` at last though the method can return the value directly as soon as it finds the key. That is because the last node list has not called the method `FindValue` so that we cannot learn whether the target key has been stored by them.
>Solved with
```go
func (node *Node) Get(key string) (bool, string) {
	// logrus.Infof("Get %s", key)
	result := node.valueLookup(key)
	if result.Found {
		return true, result.Value
	}
	var readResult ReadResult
	for _, addr := range result.NodeList {
		if err := node.RemoteCall(addr, "Kademlia.Read", RpcPair{node.addr, key}, &readResult); err == nil {
			if readResult.Ok {
				return true, readResult.Value
			}
		}
	}
	return false, ""
}
```

- **Segmentation fault**: In `set.go` we should ensure `set.list.Front() != nil` (i.e., the set is not empty) before calling its value.

- **`Set::called` and `Set::visited`**: To ensure the nodes in the returned node list are different from each other and the nodes already called won't be called again, two maps `var called, visited map[string]bool` should be recorded.

## Conclusion

The implemented Chord and Kademlia DHT protocols demonstrate robust performance, scalability, and fault tolerance. Future work will focus on optimizing network traffic, reducing latency, and exploring applications of DHTs such as decentralized file storage and real-time communication systems. This project not only solidifies the theoretical foundations of DHTs but also provides a practical framework for real-world applications.