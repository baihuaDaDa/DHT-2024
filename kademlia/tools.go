package kademlia

import (
	"crypto/sha1"
	"math/big"
)

const m int = 160

var exp [m + 1]*big.Int

func init() {
	var base = big.NewInt(2)
	exp[0] = big.NewInt(1)
	for i := 1; i <= m; i++ {
		exp[i] = new(big.Int)
		exp[i].Mul(exp[i-1], base)
	}
}

func Hash(str string) *big.Int {
	hashInstance := sha1.New()
	hashInstance.Write([]byte(str))
	bigInt := new(big.Int)
	return bigInt.SetBytes(hashInstance.Sum(nil))
}

func Locate(curId, id *big.Int) int {
	dist := new(big.Int).Xor(curId, id)
	for i := m - 1; i >= 0; i-- {
		if dist.Cmp(exp[i]) >= 0 {
			return i
		}
	}
	return -1
}
