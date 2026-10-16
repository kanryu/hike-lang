package memoryblock

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const graphHeaderBytes = 64

// GraphRelocation identifies a pointer-sized slot in the combined root and
// heap payload and the payload-relative address it must contain after restore.
type GraphRelocation struct {
	Source uint64
	Target uint64
}

// GraphObject is a heap allocation reachable from a mapped root region.
// Pointers contains byte offsets within Data and targets are object-relative
// offsets expressed through Graph.Relocations.
type GraphObject struct {
	Data []byte
}

// Graph is the compiler-facing representation of a mapped variable group.
// Root is the contiguous storage for the declared variables; Heap contains
// string and slice backing allocations in stable object order.
type Graph struct {
	Root        []byte
	Heap        []GraphObject
	Relocations []GraphRelocation
	SchemaHash  uint64
}

// SerializeGraph writes a versioned root-plus-heap image. Pointer slots are
// rewritten to payload-relative offsets and relocation records preserve the
// slots that must be fixed up after mapping. The relocation table is appended
// after the payload, then its absolute file offset and byte length are patched
// into the header after all variable-length data has been written.
func SerializeGraph(path string, graph Graph) error {
	heapBytes := 0
	for _, object := range graph.Heap {
		heapBytes += len(object.Data)
	}
	payloadBytes := len(graph.Root) + heapBytes
	if uint64(payloadBytes) > ^uint64(0)-graphHeaderBytes {
		return errors.New("memoryblock: graph payload too large")
	}
	payload := make([]byte, payloadBytes)
	copy(payload, graph.Root)
	pos := len(graph.Root)
	for _, object := range graph.Heap {
		copy(payload[pos:], object.Data)
		pos += len(object.Data)
	}
	for _, relocation := range graph.Relocations {
		if relocation.Source > uint64(payloadBytes-8) || relocation.Target >= uint64(payloadBytes) {
			return fmt.Errorf("memoryblock: graph relocation out of range: source=%d target=%d", relocation.Source, relocation.Target)
		}
		binary.LittleEndian.PutUint64(payload[relocation.Source:relocation.Source+8], relocation.Target)
	}
	header := make([]byte, graphHeaderBytes)
	copy(header[:4], "HMB3")
	binary.LittleEndian.PutUint32(header[4:8], 3)
	binary.LittleEndian.PutUint64(header[8:16], uint64(len(graph.Root)))
	binary.LittleEndian.PutUint64(header[16:24], uint64(heapBytes))
	binary.LittleEndian.PutUint64(header[32:40], uint64(payloadBytes))
	binary.LittleEndian.PutUint64(header[40:48], graph.SchemaHash)
	// [48:64] is patched after the payload and dynamic relocation table are
	// written: absolute relocation-table offset and byte length.
	relocs := make([]byte, len(graph.Relocations)*16)
	for i, relocation := range graph.Relocations {
		binary.LittleEndian.PutUint64(relocs[i*16:i*16+8], relocation.Source)
		binary.LittleEndian.PutUint64(relocs[i*16+8:i*16+16], relocation.Target)
	}
	relocationOffset := uint64(graphHeaderBytes + len(payload))
	relocationBytes := uint64(len(relocs))
	image := make([]byte, 0, len(header)+len(payload)+len(relocs))
	image = append(image, header...)
	image = append(image, payload...)
	image = append(image, relocs...)
	if err := os.WriteFile(path, image, 0o600); err != nil {
		return err
	}
	patchedHeader := append([]byte(nil), header...)
	binary.LittleEndian.PutUint64(patchedHeader[24:32], uint64(len(graph.Relocations)))
	binary.LittleEndian.PutUint64(patchedHeader[48:56], relocationOffset)
	binary.LittleEndian.PutUint64(patchedHeader[56:64], relocationBytes)
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteAt(patchedHeader, 0); err != nil {
		return err
	}
	return file.Sync()
}

// ReadGraph validates and reads an HMB3 image without applying process
// addresses. The returned payload is ready for private mapping or copying.
func ReadGraph(path string) (root, heap, payload []byte, relocations []GraphRelocation, schemaHash uint64, err error) {
	image, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil, 0, err
	}
	if len(image) < graphHeaderBytes || string(image[:4]) != "HMB3" || binary.LittleEndian.Uint32(image[4:8]) != 3 {
		return nil, nil, nil, nil, 0, errors.New("memoryblock: invalid graph header")
	}
	rootBytes := binary.LittleEndian.Uint64(image[8:16])
	heapBytes := binary.LittleEndian.Uint64(image[16:24])
	count := binary.LittleEndian.Uint64(image[24:32])
	payloadBytes := binary.LittleEndian.Uint64(image[32:40])
	schemaHash = binary.LittleEndian.Uint64(image[40:48])
	relocationOffset := binary.LittleEndian.Uint64(image[48:56])
	relocationBytes := binary.LittleEndian.Uint64(image[56:64])
	if payloadBytes != rootBytes+heapBytes || relocationBytes != count*16 || relocationOffset != uint64(graphHeaderBytes)+payloadBytes || relocationOffset > uint64(len(image)) || relocationBytes > uint64(len(image))-relocationOffset || relocationOffset+relocationBytes != uint64(len(image)) {
		return nil, nil, nil, nil, 0, errors.New("memoryblock: invalid graph size")
	}
	payload = append([]byte(nil), image[graphHeaderBytes:int(relocationOffset)]...)
	relocations = make([]GraphRelocation, int(count))
	pos := int(relocationOffset)
	for i := range relocations {
		relocations[i] = GraphRelocation{
			Source: binary.LittleEndian.Uint64(image[pos+i*16 : pos+i*16+8]),
			Target: binary.LittleEndian.Uint64(image[pos+i*16+8 : pos+i*16+16]),
		}
		if relocations[i].Source > payloadBytes-8 || relocations[i].Target >= payloadBytes {
			return nil, nil, nil, nil, 0, errors.New("memoryblock: graph relocation out of range")
		}
	}
	return payload[:rootBytes], payload[rootBytes:], payload, relocations, schemaHash, nil
}

// RestoreGraphPointers converts every serialized pointer slot to an absolute
// process address. It is intentionally separate from ReadGraph so a private
// memory mapping can be used as the backing payload.
func RestoreGraphPointers(base uintptr, payload []byte, relocations []GraphRelocation) error {
	for _, relocation := range relocations {
		if relocation.Source > uint64(len(payload)-8) || relocation.Target >= uint64(len(payload)) {
			return errors.New("memoryblock: graph relocation out of range")
		}
		binary.LittleEndian.PutUint64(payload[relocation.Source:relocation.Source+8], uint64(base)+relocation.Target)
	}
	return nil
}
