package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func TestAdhocSignatureCoversRelocatedPages(t *testing.T) {
	b := make([]byte, 8192+1024)
	le := binary.LittleEndian
	le.PutUint32(b, 0xfeedfacf)
	le.PutUint32(b[4:], 0x100000c)
	le.PutUint32(b[12:], 2)
	le.PutUint32(b[16:], 2)
	le.PutUint32(b[20:], 88)
	le.PutUint32(b[32:], 0x19)
	le.PutUint32(b[36:], 72)
	copy(b[40:], "__TEXT")
	le.PutUint64(b[56:], 0x100000000)
	le.PutUint64(b[64:], 8192)
	le.PutUint64(b[80:], 8192)
	le.PutUint32(b[104:], 0x1d)
	le.PutUint32(b[108:], 16)
	le.PutUint32(b[112:], 8192)
	le.PutUint32(b[116:], 1024)
	for i := 128; i < 8192; i++ {
		b[i] = byte(i * 7)
	}
	if e := signMachO(b); e != nil {
		t.Fatal(e)
	}
	be := binary.BigEndian
	sb := b[8192:]
	if be.Uint32(sb) != 0xfade0cc0 || be.Uint32(sb[8:]) != 1 {
		t.Fatal("invalid SuperBlob")
	}
	cd := sb[be.Uint32(sb[16:]):]
	if be.Uint32(cd) != 0xfade0c02 || be.Uint32(cd[32:]) != 8192 || be.Uint32(cd[28:]) != 2 {
		t.Fatal("invalid CodeDirectory")
	}
	hashOffset := int(be.Uint32(cd[16:]))
	for i := 0; i < 2; i++ {
		h := sha256.Sum256(b[i*4096 : (i+1)*4096])
		if !bytes.Equal(h[:], cd[hashOffset+i*32:hashOffset+(i+1)*32]) {
			t.Fatal("code page not covered")
		}
	}
	b[4096] ^= 1
	h := sha256.Sum256(b[4096:8192])
	if bytes.Equal(h[:], cd[hashOffset+32:hashOffset+64]) {
		t.Fatal("signature did not detect page change")
	}
}
