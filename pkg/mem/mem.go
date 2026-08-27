package mem

// HIKE移植時にポインタ構造体へ置き換え可能な型定義
type Allocator struct {
	AllocFn func(size uint, align uint) (*byte, error)
	FreeFn  func(ptr *byte)
	Context uintptr
}

// 静的関数として提供するメモリアロケーション
func AllocRaw(alloc Allocator, size uint, align uint) (*byte, error) {
	if alloc.AllocFn != nil {
		return alloc.AllocFn(size, align)
	}
	return nil, nil // エラー返却
}

func FreeRaw(alloc Allocator, ptr *byte) {
	if alloc.FreeFn != nil {
		alloc.FreeFn(ptr)
	}
}

// Arena アロケータ構造体
type Arena struct {
	Buffer   *byte
	Capacity uint
	Offset   uint
}

func NewArena(parent Allocator, size uint) Arena {
	// 初期段階では最小限のバッファ確保スタブ
	return Arena{
		Capacity: size,
		Offset:   0,
	}
}

func (a *Arena) FreeAll() {
	a.Offset = 0
}
