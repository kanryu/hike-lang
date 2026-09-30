package e2e_test

import "testing"

const textTemplateE2ESource = `package main

import "std/text/template"

type RuntimeConfig struct { Bits int }

func main() int {
	value := template.Render("bits={{.Bits}}", RuntimeConfig{Bits: 32})
	if value == "bits=32" { return 0 }
	return 1
}
`

func TestE2EStdTextTemplate(t *testing.T) {
	for _, goHike := range []bool{false, true} {
		name := "go"
		if goHike {
			name = "go-hike"
		}
		t.Run(name, func(t *testing.T) {
			RunHikeCase(t, HikeTestCase{
				Source:       textTemplateE2ESource,
				GoHike:       goHike,
				ExpectedExit: 0,
			})
		})
	}
}
