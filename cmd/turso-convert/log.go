package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
)

// Turso v0.6.0 core/mvcc/persistent_storage/logical_log.rs defines the LML2
// header and CRC32C-chained frames. Only no file, zero bytes, or a valid bare
// header count as empty. Even a 193-byte header-update frame is replayed;
// guessing emptiness from size could discard a small committed transaction.
// Validate every frame first: Turso itself tolerates torn tails on recovery.
func tursoLogPending(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}
	var header [56]byte
	_, err = io.ReadFull(f, header[:])
	if err != nil {
		return false, fmt.Errorf("truncated logical-log header: %w", err)
	}
	le := binary.LittleEndian
	if le.Uint32(header[:4]) != 0x4c4d4c32 || header[4] != 2 || header[5] != 0 || le.Uint16(header[6:8]) != 56 {
		return false, errors.New("unsupported or corrupt logical-log header")
	}
	for _, b := range header[16:52] {
		if b != 0 {
			return false, errors.New("corrupt logical-log reserved bytes")
		}
	}
	want := le.Uint32(header[52:])
	clear(header[52:])
	table := crc32.MakeTable(crc32.Castagnoli)
	if crc32.Checksum(header[:], table) != want {
		return false, errors.New("logical-log header checksum mismatch")
	}
	previous := crc32.Checksum(header[8:16], table)
	offset := int64(len(header))
	for offset < info.Size() {
		var frame [24]byte
		_, err = io.ReadFull(f, frame[:])
		if err != nil {
			return false, fmt.Errorf("truncated logical-log frame: %w", err)
		}
		size := le.Uint64(frame[4:12])
		if size > math.MaxInt64 {
			return false, errors.New("logical-log frame size overflows int64")
		}
		payloadSize := int64(size)
		if le.Uint32(frame[:4]) != 0x5854564d || info.Size()-offset < 32 || payloadSize > info.Size()-offset-32 {
			return false, errors.New("corrupt or truncated logical-log frame")
		}
		previous = crc32.Update(previous, table, frame[:])
		remaining := payloadSize
		var buf [32 * 1024]byte
		for remaining > 0 {
			var n int
			n, err = io.ReadFull(f, buf[:min(remaining, int64(len(buf)))])
			if err != nil {
				return false, err
			}
			previous = crc32.Update(previous, table, buf[:n])
			remaining -= int64(n)
		}
		var trailer [8]byte
		_, err = io.ReadFull(f, trailer[:])
		if err != nil {
			return false, err
		}
		if le.Uint32(trailer[:4]) != previous || le.Uint32(trailer[4:]) != 0x4554564d {
			return false, errors.New("logical-log frame checksum or trailer mismatch")
		}
		offset += 32 + payloadSize
	}
	return offset > int64(len(header)), nil
}
