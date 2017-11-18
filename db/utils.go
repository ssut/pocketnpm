package db

import "encoding/binary"

func uin32tobytes(num uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(num))
	return b
}

func bytestouint32(b []byte) uint32 {
	return binary.LittleEndian.Uint32(b)
}
