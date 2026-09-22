package wabt_test

import "testing"

func TestWabtThreadVariablesLockProtectsConcurrentUpdate(t *testing.T) {
	wasm := buildWabt(t, `package main

var concurrent(32) {
    balance int
    version int
}

func main() int {
    lock {
        balance = 100
        version = version + 1
    }
    lock {
        balance = balance + 25
        version = version + 1
    }
    return balance + version
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=127\n"; got != want {
		t.Fatalf("Wabt lock result = %q, want %q", got, want)
	}
}

func TestWabtThreadVariablesBasicStorage(t *testing.T) {
	wasm := buildWabt(t, `package main

var threadable(64) {
    workerValue int
}

var concurrent(64) {
    sharedValue int
}

func main() int {
    workerValue = 40
    sharedValue = workerValue + 2
    return sharedValue
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt threadable/concurrent basic result = %q, want %q", got, want)
	}
}

func TestWabtThreadVariablesIndependentBlockValues(t *testing.T) {
	wasm := buildWabt(t, `package main

var threadable(32) {
    first int
    second int
}

var concurrent(32) {
    shared int
}

func main() int {
    first = 10
    second = 20
    shared = first + second
    first = first + 1
    second = second + 2
    return shared + first + second
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=63\n"; got != want {
		t.Fatalf("Wabt memory block values result = %q, want %q", got, want)
	}
}

func TestWabtThreadVariablesStructValue(t *testing.T) {
	wasm := buildWabt(t, `package main

type Pair struct {
    left int
    right int
}

var threadable(64) {
    pair Pair
}

var concurrent(64) {
    checksum int
}

func main() int {
    pair.left = 7
    pair.right = 11
    checksum = pair.left + pair.right
    return checksum
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=18\n"; got != want {
		t.Fatalf("Wabt memory block struct result = %q, want %q", got, want)
	}
}
