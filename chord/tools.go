package chord

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

func Belong(target, left, right *big.Int, leftClose, rightClose bool) bool {
	lrCmp, tlCmp, trCmp := left.Cmp(right), target.Cmp(left), target.Cmp(right)
	if lrCmp > 0 {
		// fmt.Print(2)
		return (tlCmp > 0 || trCmp < 0) || (tlCmp == 0 && leftClose) || (trCmp == 0 && rightClose)
	} else if lrCmp < 0 {
		// fmt.Print(3)
		return (target.Cmp(left) > 0 && target.Cmp(right) < 0) || (tlCmp == 0 && leftClose) || (trCmp == 0 && rightClose)
	} else {
		return (tlCmp == 0 && (leftClose || rightClose)) || tlCmp != 0 // 注意这里如果左右端点重合，假如target不等于端点则always true
	}
}
