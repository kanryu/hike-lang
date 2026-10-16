package memoryblock

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const graphHeaderBytes = 48

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
// slots that must be fixed up after mapping.
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
	copy(header[:4], "HMB2")
	binary.LittleEndian.PutUint32(header[4:8], 2)
	binary.LittleEndian.PutUint64(header[8:16], uint64(len(graph.Root)))
	binary.LittleEndian.PutUint64(header[16:24], uint64(heapBytes))
	binary.LittleEndian.PutUint64(header[24:32], uint64(len(graph.Relocations)))
	binary.LittleEndian.PutUint64(header[32:40], uint64(payloadBytes))
	binary.LittleEndian.PutUint64(header[40:48], graph.SchemaHash)
	relocs := make([]byte, len(graph.Relocations)*16)
	for i, relocation := range graph.Relocations {
		binary.LittleEndian.PutUint64(relocs[i*16:i*16+8], relocation.Source)
		binary.LittleEndian.PutUint64(relocs[i*16+8:i*16+16], relocation.Target)
	}
	image := make([]byte, 0, len(header)+len(payload)+len(relocs))
	image = append(image, header...)
	image = append(image, payload...)
	image = append(image, relocs...)
	return os.WriteFile(path, image, 0o600)
}

// ReadGraph validates and reads an HMB2 image without applying process
// addresses. The returned payload is ready for private mapping or copying.
func ReadGraph(path string) (root, heap, payload []byte, relocations []GraphRelocation, schemaHash uint64, err error) {
	image, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil, 0, err
	}
	if len(image) < graphHeaderBytes || string(image[:4]) != "HMB2" || binary.LittleEndian.Uint32(image[4:8]) != 2 {
		return nil, nil, nil, nil, 0, errors.New("memoryblock: invalid graph header")
	}
	rootBytes := binary.LittleEndian.Uint64(image[8:16])
	heapBytes := binary.LittleEndian.Uint64(image[16:24])
	count := binary.LittleEndian.Uint64(image[24:32])
	payloadBytes := binary.LittleEndian.Uint64(image[32:40])
	schemaHash = binary.LittleEndian.Uint64(image[40:48])
	if payloadBytes != rootBytes+heapBytes || payloadBytes > uint64(len(image)-graphHeaderBytes) || count > uint64((len(image)-graphHeaderBytes-int(payloadBytes))/16) || graphHeaderBytes+int(payloadBytes)+int(count)*16 != len(image) {
		return nil, nil, nil, nil, 0, errors.New("memoryblock: invalid graph size")
	}
	payload = append([]byte(nil), image[graphHeaderBytes:graphHeaderBytes+int(payloadBytes)]...)
	relocations = make([]GraphRelocation, int(count))
	pos := graphHeaderBytes + int(payloadBytes)
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
