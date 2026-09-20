package hike_wabt_test

import "testing"

// These cases mirror the small, return-value-oriented portions of tests/e2e.
// Keeping the assertion at main's return value lets the same tests run through
// WAT and a standalone Wasmer process without depending on printf imports.
func TestHikeWabtCopiedE2ECases(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "arithmetic_and_branch",
			source: `package main

func main() int {
    value := 6 * 7
    if value == 42 { return value }
    return 0

}`,
			want: "42\n",
		},
		{
			name: "variables_and_array",
			source: `package main

func main() int {
    var values [4]int
    values[1] = 10
    values[2] = 32
    return values[0] + values[1] + values[2] + values[3]
}
`,
			want: "42\n",
		},
		{
			name: "user_function",
			source: `package main

func add(a int, b int) int { return a + b }

func main() int { return add(20, 22) }
`,
			want: "42\n",
		},
		{
			name: "loop_and_mutation",
			source: `package main

func main() int {
    total := 0
    for i := 1; i <= 7; i = i + 1 { total = total + i }
    return total
}
`,
			want: "28\n",
		},
		{
			name: "bitwise_operations",
			source: `package main

func main() int {
    a := 5
    b := 3
    return (a & b) + (a | b) + (a ^ b) + (a << 2) + (a >> 1)
}
`,
			want: "36\n",
		},
		{
			name: "compound_assignment",
			source: `package main

func main() int {
    value := 10
    value += 5
    value -= 2
    value *= 3
    value++
    value--
    return value
}
`,
			want: "39\n",
		},
		{
			name: "multiple_assignment",
			source: `package main

func main() int {
    a, b := 10, 20
    a, b = b, a
    return a + b
}
`,
			want: "30\n",
		},
		{
			name: "while_style_loop",
			source: `package main

func main() int {
    value := 1
    for value < 16 { value = value * 2 }
    return value
}
`,
			want: "16\n",
		},
		{
			name: "switch_cases",
			source: `package main

func main() int {
    value := 2
    switch value {
    case 1:
        return 10
    case 2, 3:
        return 42
    default:
        return 0
    }
}
`,
			want: "42\n",
		},
		{
			name: "recursion",
			source: `package main

func fib(n int) int {
    if n <= 0 { return 0 }
    if n == 1 { return 1 }
    return fib(n - 1) + fib(n - 2)
}

func main() int { return fib(10) }
`,
			want: "55\n",
		},
		{
			name: "closure_state",
			source: `package main

func makeCounter(start int) func() int {
    value := start
    return func() int {
        value = value + 1
        return value
    }
}

func main() int {
    counter := makeCounter(0)
    return counter() + counter() * 10 + counter() * 100
}
`,
			want: "321\n",
		},
		{
			name: "nested_struct_method",
			source: `package main

type Engine struct { power int }
func (e Engine) output() int { return e.power * 10 }
type Car struct { engine Engine }
func (c Car) total() int { return c.engine.output() + 50 }

func main() int {
    car := Car{engine: Engine{power: 15}}
    return car.total()
}
`,
			want: "200\n",
		},
		{
			name: "function_pointer",
			source: `package main

func add(a int, b int) int { return a + b }
func apply(fn func(int, int) int, a int, b int) int { return fn(a, b) }

func main() int {
    return apply(add, 20, 22)
}
`,
			want: "42\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildAndRunHikeWabt(t, tc.source); got != tc.want {
				t.Fatalf("Wasmer result = %q, want %q", got, tc.want)
			}
		})
	}
}
