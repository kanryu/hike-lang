module examples_gohike

hike 1.0

package ./fizzlib {
    target windows {
        hike-object: "none"
        link: "fizzlib/libfizzlib.a"
        link: "-lkernel32"
    }
}
