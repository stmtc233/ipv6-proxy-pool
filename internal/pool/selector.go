package pool

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
	"sync/atomic"
)

// Sequential 按顺序轮询，用原子计数代替互斥锁
type Sequential struct {
	n atomic.Uint64
}

func (s *Sequential) Key(*Client) Key {
	return Key{Lo: s.n.Add(1) - 1}
}

// Random 每个连接随机选择
type Random struct{}

func (Random) Key(*Client) Key {
	return randomKey()
}

func randomKey() Key {
	return Key{Hi: rand.Uint64(), Lo: rand.Uint64()}
}

// Hash 根据用户名和密码计算 Key，同一组凭据总是得到同一个出口地址
type Hash struct{}

func (Hash) Key(c *Client) Key {
	// 用户名前加长度：只用分隔符时，凭据本身含 \x00 就会出现两组凭据拼接结果相同
	b := make([]byte, 0, binary.MaxVarintLen64+len(c.Username)+len(c.Password))
	b = binary.AppendUvarint(b, uint64(len(c.Username)))
	b = append(b, c.Username...)
	b = append(b, c.Password...)
	sum := sha256.Sum256(b)
	return Key{Hi: binary.BigEndian.Uint64(sum[:8]), Lo: binary.BigEndian.Uint64(sum[8:16])}
}
