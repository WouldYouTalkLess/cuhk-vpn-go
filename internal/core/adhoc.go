// CodeDirectory layout and signing algorithm derived from the Go Authors'
// cmd/internal/codesign implementation. See LICENSE.go-codesign (BSD-3-Clause).
package core

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"fmt"
)

// Rebuild the existing Mach-O signature slot after relocation. This is the same
// SHA-256 ad-hoc CodeDirectory format used by the Go/Darwin linkers. It does not
// claim an Apple Developer ID or notarization and needs no codesign_allocate/CLT.
// Layout reference: Go cmd/internal/codesign and Apple's xnu cs_blobs.h.
func signMachO(b []byte) error {
	m, e := macho.NewFile(bytes.NewReader(b))
	if e != nil {
		return e
	}
	defer m.Close()
	var off, size int
	for _, load := range m.Loads {
		r := load.Raw()
		if len(r) >= 16 && m.ByteOrder.Uint32(r) == 0x1d {
			off = int(m.ByteOrder.Uint32(r[8:]))
			size = int(m.ByteOrder.Uint32(r[12:]))
			break
		}
	}
	if off == 0 {
		if m.Cpu == macho.CpuArm64 {
			return fmt.Errorf("ARM64 Mach-O 缺少签名区")
		}
		return nil
	}
	if off < 32 || size < 108 || off > len(b) || size > len(b)-off {
		return fmt.Errorf("无效的 Mach-O 签名区")
	}
	const page = 4096
	const directorySize = 88
	const ident = "oc"
	slots := (off + page - 1) / page
	hashOffset := directorySize + len(ident) + 1
	total := 20 + hashOffset + slots*sha256.Size
	if total > size {
		return fmt.Errorf("Mach-O 签名区空间不足")
	}
	out := b[off : off+size]
	clear(out)
	be := binary.BigEndian
	be.PutUint32(out, 0xfade0cc0)
	be.PutUint32(out[4:], uint32(total))
	be.PutUint32(out[8:], 1)
	be.PutUint32(out[12:], 0)
	be.PutUint32(out[16:], 20)
	cd := out[20:]
	be.PutUint32(cd, 0xfade0c02)
	be.PutUint32(cd[4:], uint32(total-20))
	be.PutUint32(cd[8:], 0x20400)
	be.PutUint32(cd[12:], 0x20002)
	be.PutUint32(cd[16:], uint32(hashOffset))
	be.PutUint32(cd[20:], directorySize)
	be.PutUint32(cd[28:], uint32(slots))
	be.PutUint32(cd[32:], uint32(off))
	cd[36] = 32
	cd[37] = 2
	cd[39] = 12
	if text := m.Segment("__TEXT"); text != nil {
		be.PutUint64(cd[64:], text.Offset)
		be.PutUint64(cd[72:], text.Filesz)
	}
	if m.Type == macho.TypeExec {
		be.PutUint64(cd[80:], 1)
	}
	copy(cd[directorySize:], ident)
	for slot, start := 0, 0; start < off; slot, start = slot+1, start+page {
		end := start + page
		if end > off {
			end = off
		}
		hash := sha256.Sum256(b[start:end])
		copy(cd[hashOffset+slot*32:], hash[:])
	}
	return nil
}
