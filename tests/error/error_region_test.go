package error_test

import "testing"

func TestRegionAPIWithoutAllocationMode(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

import "std/alloc/region"

func main() int {
    return region.ActiveCount()
}
`,
		ExpectedError: "region allocation API requires --alloc=region",
	})
}
