package base21

import (
	"math/big"
)

// 二十一进制字母表（21 个字符）
const alphabet = "0123456789ABCDEFGHIJK"

// EncodeToString 将字节序列编码为二十一进制字符串
func EncodeToString(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	n := new(big.Int).SetBytes(data)
	base := big.NewInt(21)
	zero := big.NewInt(0)
	var out []byte
	for n.Cmp(zero) > 0 {
		mod := new(big.Int)
		n.DivMod(n, base, mod)
		out = append([]byte{alphabet[mod.Int64()]}, out...)
	}
	return string(out)
}
