package region_test

import "testing"

func TestRegion_APIRequiresRegionMode(t *testing.T) {
	runRegionCaseWithMode(t, regionCase{
		source: `package main
import "std/alloc/region"

func main() int {
    return region.ActiveCount()
}`,
		want: "",
	}, false)
}

func TestRegion_ClosureCapture(t *testing.T) {
	runRegionCase(t, regionCase{
		source: `package main
import "std/alloc/region"
func printf(format string, ...) int

func exercise() int {
    base := 10
    add := func(n int) int {
        base = base + n
        return base
    }
    p := &Point{value: add(2)}
    return p.value + add(3) - 10
}

type Point struct { value int }

func main() int {
    active0 := region.ActiveCount()
    begin0 := region.BeginCount()
    end0 := region.EndCount()
    allocated0 := region.AllocatedBytes()
    released0 := region.ReleasedBytes()
    result := exercise()
    if result != 17 { return 2 }
    if region.ActiveCount() != active0 { return 3 }
    if region.BeginCount() - begin0 != region.EndCount() - end0 { return 4 }
    if region.ReleasedBytes() - released0 < region.AllocatedBytes() - allocated0 { return 5 }
    printf("%d\n", result)
    return 0
}`,
		want: "17",
	})
}

func TestRegion_ForLoopAllocatesRepeatedValues(t *testing.T) {
	runRegionCase(t, regionCase{
		source: `package main
import "std/alloc/region"
func printf(format string, ...) int

type Point struct { x int, y int }

func exercise() int {
    total := 0
    for i := 1; i <= 4; i = i + 1 {
        p := &Point{x: i, y: i * 2}
        total = total + p.x + p.y
    }
    return total
}

func main() int {
    active0 := region.ActiveCount()
    begin0 := region.BeginCount()
    end0 := region.EndCount()
    allocated0 := region.AllocatedBytes()
    released0 := region.ReleasedBytes()
    result := exercise()
    if region.ActiveCount() != active0 { return 2 }
    if region.BeginCount() - begin0 != region.EndCount() - end0 { return 3 }
    if region.ReleasedBytes() - released0 < region.AllocatedBytes() - allocated0 { return 4 }
    printf("%d\n", result)
    return 0
}`,
		want: "30",
	})
}

func TestRegion_PrimitiveAndStructPointers(t *testing.T) {
	runRegionCase(t, regionCase{
		source: `package main
import "std/alloc/region"
func printf(format string, ...) int

type Number struct { value int }

func exercise() int {
    n := 7
    np := &n
    item := &Number{value: 35}
    *np = *np + item.value
    return *np + item.value
}

func main() int {
    active0 := region.ActiveCount()
    begin0 := region.BeginCount()
    end0 := region.EndCount()
    allocated0 := region.AllocatedBytes()
    released0 := region.ReleasedBytes()
    result := exercise()
    if result != 77 { return 2 }
    if region.ActiveCount() != active0 { return 3 }
    if region.BeginCount() - begin0 != region.EndCount() - end0 { return 4 }
    if region.ReleasedBytes() - released0 < region.AllocatedBytes() - allocated0 { return 5 }
    printf("%d,%d\n", result - 35, 35)
    return 0
}`,
		want: "42,35",
	})
}

func TestRegion_ReleaseAndExplicitHeapFree(t *testing.T) {
	runRegionCase(t, regionCase{
		source: `package main
import "std/alloc/region"
func printf(format string, ...) int
func malloc(size int) *byte
func free(ptr *byte)

type Point struct { value int }

func makePoint() *Point {
    return &Point{value: 9}
}

func exercise() int {
    p := makePoint()
    raw := malloc(16)
    free(raw)
    return p.value
}

func main() int {
    active0 := region.ActiveCount()
    begin0 := region.BeginCount()
    end0 := region.EndCount()
    allocated0 := region.AllocatedBytes()
    released0 := region.ReleasedBytes()
    result := exercise()
    if result != 9 { return 2 }
    if region.ActiveCount() != active0 { return 3 }
    if region.BeginCount() - begin0 != region.EndCount() - end0 { return 4 }
    if region.ReleasedBytes() - released0 < region.AllocatedBytes() - allocated0 { return 5 }
    printf("%d\n", result)
    return 0
}`,
		want: "9",
	})
}
